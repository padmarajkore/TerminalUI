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

type clientState struct {
	isAlive       bool
	lastHeartbeat time.Time
}

type logManager struct {
	mu   sync.Mutex
	logs []string
}

func (lm *logManager) AddLog(log string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.logs = append(lm.logs, log)
}

func (lm *logManager) GetFilteredLogs(filter string) []string {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	if filter == "ALL" {
		return append([]string(nil), lm.logs...)
	}
	filteredLogs := []string{}
	for _, log := range lm.logs {
		if strings.Contains(strings.ToUpper(log), strings.ToUpper(filter)) {
			filteredLogs = append(filteredLogs, log)
		}
	}
	return filteredLogs
}

func (lm *logManager) GetSearchFilteredLogs(query string) []string {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	if query == "" {
		return append([]string(nil), lm.logs...)
	}
	filteredLogs := []string{}
	for _, log := range lm.logs {
		if strings.Contains(strings.ToLower(log), strings.ToLower(query)) {
			filteredLogs = append(filteredLogs, log)
		}
	}
	return filteredLogs
}

func main() {
	app := tview.NewApplication()
	var clientConn net.Conn
	var clientAlive bool
	var lastHeartbeat time.Time
	var connMutex sync.Mutex
	var currentFilter = "ALL" // Moved to higher scope to be used in handleClient

	app.EnableMouse(true)

	// UI Components
	logoView := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetText("[yellow]SERVER LOGGER[white]")

	logsView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)

	connectionStatus := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetText("No Client Connected")

	footer := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetText("Press '/' to focus Search Bar, 'Q' to Quit")

	searchBar := tview.NewInputField().
		SetLabel("Search: ").
		SetFieldWidth(30).
		SetPlaceholder("Type here to filter logs...")

	// Create filter buttons
	logManager := &logManager{}

	createFilterButton := func(label, filter string) *tview.Button {
		button := tview.NewButton(label).
			SetSelectedFunc(func() {
				currentFilter = filter
				filteredLogs := logManager.GetFilteredLogs(filter)
				logsView.SetText(fmt.Sprintf("Current Filter: %s\n\n%s",
					filter, strings.Join(filteredLogs, "\n")))
			})

		// Add visual feedback for button states
		button.SetBlurFunc(func() {
			button.SetBackgroundColor(tcell.ColorDefault)
		})

		button.SetFocusFunc(func() {
			button.SetBackgroundColor(tcell.ColorDarkBlue)
		})

		return button
	}

	// Create buttons with different colors and clear labels
	allButton := createFilterButton("📋 [white]All", "ALL")
	infoButton := createFilterButton("ℹ️ [green]Info", "INFO")
	warningButton := createFilterButton("⚠️ [yellow]Warning", "WARNING")
	errorButton := createFilterButton("❌ [red]Error", "ERROR")

	// Add buttons to a horizontal flex container with some padding
	buttonFlex := tview.NewFlex().
		SetDirection(tview.FlexRow)

	// Create a flex for buttons with padding
	buttonRow := tview.NewFlex().
		AddItem(nil, 1, 0, false).
		AddItem(allButton, 0, 1, true).
		AddItem(nil, 1, 0, false).
		AddItem(infoButton, 0, 1, true).
		AddItem(nil, 1, 0, false).
		AddItem(warningButton, 0, 1, true).
		AddItem(nil, 1, 0, false).
		AddItem(errorButton, 0, 1, true).
		AddItem(nil, 1, 0, false)

	buttonFlex.AddItem(nil, 0, 1, false).
		AddItem(buttonRow, 1, 0, true).
		AddItem(nil, 0, 1, false)

	footer.SetText("Mouse: Click buttons to filter | Keyboard: TAB to navigate, ENTER to select | '/' Search, 'Q' Quit")

	// Main grid layout
	grid := tview.NewGrid().
		SetRows(1, 1, 3, 0, 1, 1).  // Increased button row height
		SetColumns(0).
		SetBorders(true)

		grid.AddItem(logoView, 0, 0, 1, 1, 0, 0, false).
		AddItem(searchBar, 1, 0, 1, 1, 0, 0, false).
		AddItem(buttonFlex, 2, 0, 1, 1, 0, 0, true).
		AddItem(logsView, 3, 0, 1, 1, 0, 0, false).
		AddItem(connectionStatus, 4, 0, 1, 1, 0, 0, false).
		AddItem(footer, 5, 0, 1, 1, 0, 0, false)

	// Monitor client connection status with blinking emoji
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		showEmoji := true
		for range ticker.C {
			connMutex.Lock()
			if clientConn != nil && time.Since(lastHeartbeat) > 3*time.Second {
				clientAlive = false
			}
			isConnected := clientConn != nil && clientAlive
			connMutex.Unlock()

			app.QueueUpdateDraw(func() {
				if isConnected {
					if showEmoji {
						connectionStatus.SetText("Client Connected")
					} else {
						connectionStatus.SetText(aliveASCII + " Client Connected")
					}
				} else {
					connectionStatus.SetText(brokenASCII + " No Client Connected")
				}
			})
			showEmoji = !showEmoji
		}
	}()

	// Start server
	ln, err := net.Listen("tcp", ":8080")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
		return
	}
	defer ln.Close()

	// Accept client connections
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error accepting connection: %v\n", err)
				continue
			}

			connMutex.Lock()
			if clientConn != nil {
				clientConn.Close()
			}
			clientConn = conn
			clientAlive = true
			lastHeartbeat = time.Now()
			connMutex.Unlock()

			go handleClient(conn, logManager, logsView, app, &connMutex, &clientAlive, &lastHeartbeat, &currentFilter)
		}
	}()

	// Handle keyboard inputs
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case '/':
				app.SetFocus(searchBar)
				return nil
			case 'q', 'Q':
				app.Stop()
				return nil
			}
		}
		return event
	})

	// Search bar functionality
	searchBar.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			app.SetFocus(buttonFlex) // Return focus to buttons after search
		}
	})
	searchBar.SetChangedFunc(func(query string) {
		filteredLogs := logManager.GetSearchFilteredLogs(query)
		logsView.SetText(fmt.Sprintf("Search Query: %s\n\n%s",
			query, strings.Join(filteredLogs, "\n")))
	})

	if err := app.SetRoot(grid, true).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running app: %v\n", err)
	}
}

func handleClient(
	conn net.Conn,
	logManager *logManager,
	logsView *tview.TextView,
	app *tview.Application,
	connMutex *sync.Mutex,
	clientAlive *bool,
	lastHeartbeat *time.Time,
	currentFilter *string,
) {
	defer func() {
		connMutex.Lock()
		conn.Close()
		*clientAlive = false
		connMutex.Unlock()
	}()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		message := scanner.Text()

		if message == heartbeat {
			connMutex.Lock()
			*clientAlive = true
			*lastHeartbeat = time.Now()
			connMutex.Unlock()
			continue
		}

		logManager.AddLog(colorizeLog(message))
		app.QueueUpdateDraw(func() {
			logsView.SetText(fmt.Sprintf("Current Filter: %s\n\n%s",
				*currentFilter, strings.Join(logManager.GetFilteredLogs(*currentFilter), "\n")))
		})
	}
}
func colorizeLog(log string) string {
	if strings.Contains(log, "INFO") {
		return fmt.Sprintf("[green]%s[white]", log)
	} else if strings.Contains(log, "WARNING") {
		return fmt.Sprintf("[yellow]%s[white]", log)
	} else if strings.Contains(log, "ERROR") {
		return fmt.Sprintf("[red]%s[white]", log)
	}
	return log
}
