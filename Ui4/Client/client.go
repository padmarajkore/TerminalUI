package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	aliveASCII  = "🟢"
	brokenASCII = "🔴"
	heartbeat   = "_HEARTBEAT_"
)

type logManager struct {
	mu   sync.Mutex
	logs []string
}

func (lm *logManager) AddLog(log string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.logs = append(lm.logs, log)
}

func (lm *logManager) GetLogs(limit int) []string {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	if len(lm.logs) > limit {
		return lm.logs[len(lm.logs)-limit:]
	}
	return append([]string(nil), lm.logs...)
}

func main() {
	app := tview.NewApplication()

	// UI Components
	logoView := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetText("[yellow]CLIENT LOGGER APP[white]")

	logsView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetChangedFunc(func() { app.Draw() })

	connectionStatus := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)

	footer := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetText("Press 'I' (Info), 'W' (Warning), 'E' (Error), 'Q' (Quit)")

	grid := tview.NewGrid().
		SetRows(1, 0, 1, 1).
		SetColumns(0).
		SetBorders(true)

	grid.AddItem(logoView, 0, 0, 1, 1, 0, 0, false).
		AddItem(logsView, 1, 0, 1, 1, 0, 0, true).
		AddItem(connectionStatus, 2, 0, 1, 1, 0, 0, false).
		AddItem(footer, 3, 0, 1, 1, 0, 0, false)

	logManager := &logManager{}
	logLimit := 50

	// Connection state
	var connMutex sync.Mutex
	var isConnected bool
	var conn net.Conn

	// Function to check connection
	checkConnection := func() bool {
		connMutex.Lock()
		defer connMutex.Unlock()

		if conn == nil {
			isConnected = false
			return false
		}

		// Try to write a heartbeat to check connection
		_, err := conn.Write([]byte("\n"))
		if err != nil {
			conn.Close()
			conn = nil
			isConnected = false
			return false
		}
		isConnected = true
		return true
	}

	// Function to attempt connection
	connect := func() bool {
		connMutex.Lock()
		defer connMutex.Unlock()

		if conn != nil {
			conn.Close()
			conn = nil
		}

		newConn, err := net.Dial("tcp", "localhost:8080")
		if err != nil {
			isConnected = false
			return false
		}
		conn = newConn
		isConnected = true
		return true
	}

	// Initial connection
	connect()

	// Create channels for communication
	logChan := make(chan string)
	heartbeatChan := make(chan struct{})

	// Message sender goroutine
	go func() {
		for {
			select {
			case logMsg := <-logChan:
				connMutex.Lock()
				if conn != nil && isConnected {
					writer := bufio.NewWriter(conn)
					_, err := writer.WriteString(logMsg + "\n")
					if err != nil {
						conn.Close()
						conn = nil
						isConnected = false
					} else {
						err = writer.Flush()
						if err != nil {
							conn.Close()
							conn = nil
							isConnected = false
						}
					}
				}
				connMutex.Unlock()
			case <-heartbeatChan:
				connMutex.Lock()
				if conn != nil && isConnected {
					writer := bufio.NewWriter(conn)
					_, err := writer.WriteString(heartbeat + "\n")
					if err != nil {
						conn.Close()
						conn = nil
						isConnected = false
					} else {
						err = writer.Flush()
						if err != nil {
							conn.Close()
							conn = nil
							isConnected = false
						}
					}
				}
				connMutex.Unlock()
			}
		}
	}()

	// Heartbeat and connection checker
	go func() {
		heartbeatTicker := time.NewTicker(1 * time.Second)
		blinkTicker := time.NewTicker(500 * time.Millisecond)
		showEmoji := true

		for {
			select {
			case <-heartbeatTicker.C:
				if !checkConnection() {
					// Try to reconnect if connection is lost
					connect()
				}
				heartbeatChan <- struct{}{}

			case <-blinkTicker.C:
				connStatus := isConnected
				app.QueueUpdateDraw(func() {
					if connStatus {
						if showEmoji {
							connectionStatus.SetText("Connected")
						} else {
							connectionStatus.SetText(aliveASCII + " Connected")
						}
					} else {
						connectionStatus.SetText(brokenASCII + " Disconnected")
					}
				})
				showEmoji = !showEmoji
			}
		}
	}()

	// Handle keypresses
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if !isConnected {
			logManager.AddLog("Connection is broken. Unable to send log.")
			updateLogsView(logsView, logManager, logLimit)
			return nil
		}

		var logMsg string
		timestamp := time.Now().Format("2006-01-02 15:04:05")

		switch event.Rune() {
		case 'I', 'i':
			logMsg = fmt.Sprintf("%s INFO: Info log sent", timestamp)
		case 'W', 'w':
			logMsg = fmt.Sprintf("%s WARNING: Warning log sent", timestamp)
		case 'E', 'e':
			logMsg = fmt.Sprintf("%s ERROR: Error log sent", timestamp)
		case 'Q', 'q':
			app.Stop()
			return nil
		default:
			return event
		}

		logManager.AddLog(logMsg)
		updateLogsView(logsView, logManager, logLimit)

		select {
		case logChan <- logMsg:
			// Log sent successfully
		default:
			logManager.AddLog("Failed to send log to server.")
			updateLogsView(logsView, logManager, logLimit)
		}

		return nil
	})

	if err := app.SetRoot(grid, true).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running app: %v\n", err)
	}
}

func updateLogsView(view *tview.TextView, manager *logManager, limit int) {
	view.Clear()
	logs := manager.GetLogs(limit)

	var colorizedLogs []string
	for _, log := range logs {
		if strings.Contains(log, "INFO") {
			colorizedLogs = append(colorizedLogs, fmt.Sprintf("[green]%s[white]", log))
		} else if strings.Contains(log, "WARNING") {
			colorizedLogs = append(colorizedLogs, fmt.Sprintf("[yellow]%s[white]", log))
		} else if strings.Contains(log, "ERROR") {
			colorizedLogs = append(colorizedLogs, fmt.Sprintf("[red]%s[white]", log))
		} else {
			colorizedLogs = append(colorizedLogs, log)
		}
	}

	view.SetText(strings.Join(colorizedLogs, "\n"))
}
