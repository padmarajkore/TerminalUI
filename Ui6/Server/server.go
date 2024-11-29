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
	aliveASCII     = "🟢"
	brokenASCII    = "🔴"
	heartbeat      = "_HEARTBEAT_"
	serverPort     = ":8080"
	heartbeatTimer = 3 * time.Second
)

type LogManager struct {
	mu   sync.Mutex
	logs []string
}

func (lm *LogManager) AddLog(log string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.logs = append(lm.logs, log)
}

func (lm *LogManager) GetSearchFilteredLogs(query string, logType string) []string {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	var filteredLogs []string
	for _, log := range lm.logs {
		if strings.Contains(strings.ToUpper(log), strings.ToUpper(logType)) {
			if query == "" || strings.Contains(strings.ToLower(log), strings.ToLower(query)) {
				filteredLogs = append(filteredLogs, log)
			}
		}
	}
	return filteredLogs
}

type ConnectionState struct {
	conn          net.Conn
	alive         bool
	lastHeartbeat time.Time
	mu            sync.Mutex
}

func NewConnectionState() *ConnectionState {
	return &ConnectionState{}
}

type UIComponents struct {
	app              *tview.Application
	grid             *tview.Grid
	logoView         *tview.TextView
	infoLogsView     *tview.TextView
	warningLogsView  *tview.TextView
	errorLogsView    *tview.TextView
	searchBar        *tview.InputField
	connectionStatus *tview.TextView
	footer           *tview.TextView
}

func CreateUIComponents() *UIComponents {
	ui := &UIComponents{
		app: tview.NewApplication(),
	}

	ui.logoView = tview.NewTextView()
	ui.logoView.
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetText("[yellow]SERVER LOGGER[white]")

	ui.infoLogsView = tview.NewTextView()
	ui.infoLogsView.
		SetDynamicColors(true).
		SetScrollable(true).
		SetBorder(true).
		SetTitle("ℹ️ Info Logs")

	ui.warningLogsView = tview.NewTextView()
	ui.warningLogsView.
		SetDynamicColors(true).
		SetScrollable(true).
		SetBorder(true).
		SetTitle("⚠️ Warning Logs")

	ui.errorLogsView = tview.NewTextView()
	ui.errorLogsView.
		SetDynamicColors(true).
		SetScrollable(true).
		SetBorder(true).
		SetTitle("❌ Error Logs")

	ui.searchBar = tview.NewInputField()
	ui.searchBar.
		SetLabel("Search: ").
		SetFieldWidth(30).
		SetPlaceholder("Type here to filter logs...").
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEsc {
				ui.searchBar.SetText("")
				ui.app.SetFocus(ui.grid)
			}
		})

	ui.connectionStatus = tview.NewTextView()
	ui.connectionStatus.
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetText("No Client Connected")

	ui.footer = tview.NewTextView()
	ui.footer.
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetText("Mouse: Use search to filter logs | '/' Search, 'Q' Quit")

	return ui
}

func main() {
	logManager := &LogManager{}
	connState := NewConnectionState()
	ui := CreateUIComponents()

	ui.grid = tview.NewGrid().
		SetRows(1, 1, 0, 1, 1).
		SetColumns(0, 0, 0).
		SetBorders(true)

	ui.grid.AddItem(ui.logoView, 0, 0, 1, 3, 0, 0, false).
		AddItem(ui.searchBar, 1, 0, 1, 3, 0, 0, false).
		AddItem(ui.infoLogsView, 2, 0, 1, 1, 0, 0, false).
		AddItem(ui.warningLogsView, 2, 1, 1, 1, 0, 0, false).
		AddItem(ui.errorLogsView, 2, 2, 1, 1, 0, 0, false).
		AddItem(ui.connectionStatus, 3, 0, 1, 3, 0, 0, false).
		AddItem(ui.footer, 4, 0, 1, 3, 0, 0, false)

	updateLogSections := func(searchQuery string) {
		ui.app.QueueUpdateDraw(func() {
			ui.infoLogsView.SetText(strings.Join(logManager.GetSearchFilteredLogs(searchQuery, "INFO"), "\n"))
			ui.warningLogsView.SetText(strings.Join(logManager.GetSearchFilteredLogs(searchQuery, "WARNING"), "\n"))
			ui.errorLogsView.SetText(strings.Join(logManager.GetSearchFilteredLogs(searchQuery, "ERROR"), "\n"))
		})
	}

	ui.searchBar.SetChangedFunc(func(query string) {
		updateLogSections(query)
	})

	ui.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case '/':
				ui.app.SetFocus(ui.searchBar)
			case 'q', 'Q':
				ui.app.Stop()
			}
		case tcell.KeyEsc:
			ui.searchBar.SetText("")
			ui.app.SetFocus(ui.grid)
		}
		return event
	})

	go monitorConnection(ui, connState)
	go acceptConnections(connState, logManager, updateLogSections)

	if err := ui.app.SetRoot(ui.grid, true).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running application: %v\n", err)
	}
}

func monitorConnection(ui *UIComponents, connState *ConnectionState) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	showEmoji := true
	for range ticker.C {
		connState.mu.Lock()
		if connState.conn != nil && time.Since(connState.lastHeartbeat) > heartbeatTimer {
			connState.alive = false
		}
		isConnected := connState.conn != nil && connState.alive
		connState.mu.Unlock()

		ui.app.QueueUpdateDraw(func() {
			if isConnected {
				if showEmoji {
					ui.connectionStatus.SetText("Client Connected")
				} else {
					ui.connectionStatus.SetText(aliveASCII + " Client Connected")
				}
			} else {
				ui.connectionStatus.SetText(brokenASCII + " No Client Connected")
			}
		})
		showEmoji = !showEmoji
	}
}

func acceptConnections(connState *ConnectionState, logManager *LogManager, updateLogSections func(string)) {
	ln, err := net.Listen("tcp", serverPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
		return
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error accepting connection: %v\n", err)
			continue
		}

		connState.mu.Lock()
		if connState.conn != nil {
			connState.conn.Close()
		}
		connState.conn = conn
		connState.alive = true
		connState.lastHeartbeat = time.Now()
		connState.mu.Unlock()

		go handleClient(conn, connState, logManager, updateLogSections)
	}
}

func handleClient(conn net.Conn, connState *ConnectionState, logManager *LogManager, updateLogSections func(string)) {
	defer func() {
		connState.mu.Lock()
		conn.Close()
		connState.alive = false
		connState.mu.Unlock()
	}()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		message := scanner.Text()
		if message == heartbeat {
			connState.mu.Lock()
			connState.alive = true
			connState.lastHeartbeat = time.Now()
			connState.mu.Unlock()
			continue
		}
		logManager.AddLog(colorizeLog(message))
		updateLogSections("")
	}
}

func colorizeLog(log string) string {
	switch {
	case strings.Contains(log, "INFO"):
		return fmt.Sprintf("[green]%s[white]", log)
	case strings.Contains(log, "WARNING"):
		return fmt.Sprintf("[yellow]%s[white]", log)
	case strings.Contains(log, "ERROR"):
		return fmt.Sprintf("[red]%s[white]", log)
	default:
		return log
	}
}
