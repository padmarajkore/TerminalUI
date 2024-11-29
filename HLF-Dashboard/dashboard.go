package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	NETWORK_SCRIPT = "/home/fabric-samples/test-network/network.sh"
	CHAINCODE_NAME = "basic"
	CHAINCODE_PATH = "../asset-transfer-basic/chaincode-go"
	CHAINCODE_LANG = "go"
)

func main() {
	app := tview.NewApplication()

	// Create main layout
	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow)

	// Create buttons panel
	buttonFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	buttonFlex.SetBorder(true).SetTitle("[::u]Network Operations").SetBorderColor(tcell.ColorBlue)

	// Create text view for logs
	logView := tview.NewTextView().
		SetDynamicColors(true).
		SetChangedFunc(func() {
			app.Draw()
		})
	logView.SetBorder(true).SetTitle("[::u]Logs").SetBorderColor(tcell.ColorGreen)
	logView.SetScrollable(true).SetWordWrap(true)

	// Create help & instructions view
	helpView := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft).
		SetScrollable(false).
		SetWrap(false)
	helpView.SetBorder(true).SetTitle("[::u]Help & Instructions").SetBorderColor(tcell.ColorYellow)

	// Instruction message
	helpMessage := `[yellow]Instructions: [white]Use [lime]Tab[white] to switch focus, [lime]Enter[white] to execute, and [lime]Esc[white] to exit. Commands: [lime]Up[white]=Start Network, [lime]Down[white]=Stop Network, [lime]Deploy[white]=Deploy Chaincode, [lime]Clear[white]=Logs. Logs: [lime]Green[white]=Success, [red]Red[white]=Error, [yellow]Yellow[white]=Info.`
	paddingWidth := 50
	paddedMessage := fmt.Sprintf("%s%s", helpMessage, strings.Repeat(" ", paddingWidth))

	// Function to scroll text horizontally
	go func() {
		for {
			helpView.SetText(paddedMessage)
			time.Sleep(100 * time.Millisecond)
			paddedMessage = paddedMessage[1:] + paddedMessage[:1]
			app.Draw()
		}
	}()

	// Function to append logs with timestamp and color coding
	appendLog := func(text string, logType string) {
		timestamp := time.Now().Format("15:04:05")
		var coloredText string
		switch logType {
		case "info":
			coloredText = fmt.Sprintf("[yellow]%s │[white] %s", timestamp, text)
		case "success":
			coloredText = fmt.Sprintf("[lime]%s │ %s[white]", timestamp, text)
		case "error":
			coloredText = fmt.Sprintf("[red]%s │ %s[white]", timestamp, text)
		case "system":
			coloredText = fmt.Sprintf("[blue]%s │ %s[white]", timestamp, text)
		case "chaincode":
			coloredText = fmt.Sprintf("[yellow]%s │[orange] %s[white]", timestamp, text)
		case "peer":
			coloredText = fmt.Sprintf("[yellow]%s │[cyan] %s[white]", timestamp, text)
		default:
			coloredText = fmt.Sprintf("[white]%s │ %s", timestamp, text)
		}
		logView.SetText(logView.GetText(true) + coloredText + "\n")
		logView.ScrollToEnd()
	}

	// Function to execute a command and append output to logs
	executeCommand := func(args ...string) {
		cmd := exec.Command(NETWORK_SCRIPT, args...)
		cmd.Dir = "/home/fabric-samples/test-network"

		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			appendLog(fmt.Sprintf("Error creating stdout pipe: %v", err), "error")
			return
		}

		stderrPipe, err := cmd.StderrPipe()
		if err != nil {
			appendLog(fmt.Sprintf("Error creating stderr pipe: %v", err), "error")
			return
		}

		appendLog(fmt.Sprintf("Executing: %s %s", NETWORK_SCRIPT, strings.Join(args, " ")), "system")

		// Start the command
		if err := cmd.Start(); err != nil {
			appendLog(fmt.Sprintf("Error starting command: %v", err), "error")
			return
		}

		// Create channels for live output
		stdoutChan := make(chan string)
		stderrChan := make(chan string)

		// Read stdout asynchronously
		go func() {
			scanner := bufio.NewScanner(stdoutPipe)
			for scanner.Scan() {
				stdoutChan <- scanner.Text()
			}
			close(stdoutChan)
		}()

		// Read stderr asynchronously
		go func() {
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				stderrChan <- scanner.Text()
			}
			close(stderrChan)
		}()

		// Process output in real-time
		done := make(chan bool)
		go func() {
			for {
				select {
				case line, ok := <-stdoutChan:
					if !ok {
						stdoutChan = nil
					} else {
						appendLog(line, "info")
					}
				case line, ok := <-stderrChan:
					if !ok {
						stderrChan = nil
					} else {
						appendLog(line, "error")
					}
				}

				// Exit when both channels are closed
				if stdoutChan == nil && stderrChan == nil {
					done <- true
					return
				}
			}
		}()

		<-done

		// Wait for the command to finish
		if err := cmd.Wait(); err != nil {
			appendLog(fmt.Sprintf("Command finished with error: %v", err), "error")
		} else {
			appendLog("Command completed successfully", "success")
		}
	}

	// Improved fetchPeerLogs function
	fetchPeerLogs := func(peerName string) {
		appendLog(fmt.Sprintf("Fetching logs for peer: %s", peerName), "peer")

		// Check if container exists and is running
		checkCmd := exec.Command("docker", "ps", "--format", "{{.Names}}", "--filter", fmt.Sprintf("name=%s", peerName))
		checkOutput, err := checkCmd.CombinedOutput()
		if err != nil || len(checkOutput) == 0 {
			appendLog(fmt.Sprintf("Error: Peer container %s is not running", peerName), "error")
			return
		}

		// Execute docker logs command with proper parameters
		cmd := exec.Command("docker", "logs", "--tail", "1000", "--timestamps", peerName)

		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf

		err = cmd.Run()
		if err != nil {
			appendLog(fmt.Sprintf("Error executing docker logs command: %v", err), "error")
			if errContent := errBuf.String(); errContent != "" {
				appendLog(fmt.Sprintf("Docker error output: %s", errContent), "error")
			}
			return
		}

		// Process stdout logs
		logs := outBuf.String()
		if logs == "" {
			appendLog(fmt.Sprintf("No stdout logs found for peer %s", peerName), "info")
		} else {
			appendLog("Found logs for peer. Processing...", "info")
			logLines := strings.Split(logs, "\n")
			for _, line := range logLines {
				if strings.TrimSpace(line) != "" {
					appendLog(strings.TrimSpace(line), "peer")
				}
			}
		}

		// Process stderr logs if any
		errLogs := errBuf.String()
		if errLogs != "" {
			appendLog("Processing error logs...", "info")
			errLogLines := strings.Split(errLogs, "\n")
			for _, line := range errLogLines {
				if strings.TrimSpace(line) != "" {
					appendLog(line, "error")
				}
			}
		}

		appendLog("Finished fetching logs", "success")
	}

	// Define peer containers mapping
	peerContainers := map[string]string{
		"Org1 Peer0": "peer0.org1.example.com",
		// "Org1 Peer1": "peer1.org1.example.com",
		"Org2 Peer0": "peer0.org2.example.com",
		// "Org2 Peer1": "peer1.org2.example.com",
	}

	// Create peer options for dropdown
	peerOptions := make([]string, 0, len(peerContainers))
	for label := range peerContainers {
		peerOptions = append(peerOptions, label)
	}

	// Create improved dropdown for peer logs
	peerDropdown := tview.NewDropDown().
		SetLabel("Select Peer: ").
		SetOptions(peerOptions, func(option string, index int) {
			if containerName, ok := peerContainers[option]; ok {
				logView.Clear()
				appendLog(fmt.Sprintf("Selected peer: %s (%s)", option, containerName), "system")
				go func() {
					fetchPeerLogs(containerName)
				}()
			}
		})
	peerDropdown.SetBorder(true).SetTitle("Peer Logs")

	// Create buttons
	networkUpBtn := tview.NewButton("Network Up").
		SetSelectedFunc(func() {
			go executeCommand("up", "createChannel")
		})

	networkDownBtn := tview.NewButton("Network Down").
		SetSelectedFunc(func() {
			go executeCommand("down")
		})

	deployChaincodeBtn := tview.NewButton("Deploy Chaincode").
		SetSelectedFunc(func() {
			go func() {
				appendLog("Starting chaincode deployment process...", "chaincode")
				executeCommand("deployCC",
					"-ccn", CHAINCODE_NAME,
					"-ccp", CHAINCODE_PATH,
					"-ccl", CHAINCODE_LANG)
			}()
		})

	fetchNetworkSpecs := func() string {
		var specs strings.Builder
		specs.WriteString("=== Network Specifications ===\n")

		// Fetch peers and map them to organizations
		cmd := exec.Command("docker", "ps", "--format", "{{.Names}}", "--filter", "name=peer")
		output, err := cmd.Output()
		if err != nil {
			return fmt.Sprintf("Error fetching peers: %v\n", err)
		}

		peers := strings.Split(strings.TrimSpace(string(output)), "\n")
		orgCounts := map[string]int{}

		for _, peer := range peers {
			parts := strings.Split(peer, ".")
			if len(parts) > 1 {
				org := parts[1]
				orgCounts[org]++
			}
		}

		for org, count := range orgCounts {
			specs.WriteString(fmt.Sprintf("Organization: %s, Peer Count: %d\n", org, count))
		}

		return specs.String()
	}

	fetchInstalledChaincodes := func() string {
		cmd := exec.Command("peer", "lifecycle", "chaincode", "queryinstalled")
		cmd.Env = append(cmd.Env, "FABRIC_CFG_PATH=/home/fabric-samples/config") // Ensure the correct environment

		output, err := cmd.Output()
		if err != nil {
			return fmt.Sprintf("Error fetching chaincodes: %v\n", err)
		}

		return fmt.Sprintf("=== Installed Chaincodes ===\n%s", string(output))
	}

	fetchHLFNetworkInfo := func() string {
		var info strings.Builder
		info.WriteString(fetchNetworkSpecs())
		info.WriteString("\n")
		info.WriteString(fetchInstalledChaincodes())
		return info.String()
	}

	networkInfoBtn := tview.NewButton("Show Network Info").
		SetSelectedFunc(func() {
			go func() {
				logView.Clear()
				appendLog("Fetching network specifications...", "system")
				networkInfo := fetchHLFNetworkInfo()
				appendLog(networkInfo, "info")
			}()
		})

	clearLogsBtn := tview.NewButton("Clear Logs").
		SetSelectedFunc(func() {
			logView.SetText("")
			appendLog("Logs cleared", "system")
		})

	// Add buttons to the button panel
	buttonFlex.AddItem(networkUpBtn, 0, 1, true)
	buttonFlex.AddItem(networkDownBtn, 0, 1, true)
	buttonFlex.AddItem(deployChaincodeBtn, 0, 1, true)
	buttonFlex.AddItem(clearLogsBtn, 0, 1, true)
	buttonFlex.AddItem(networkInfoBtn, 0, 1, true)

	// Layout setup
	mainFlex.AddItem(buttonFlex, 5, 1, true)
	mainFlex.AddItem(peerDropdown, 3, 0, false)
	mainFlex.AddItem(logView, 0, 2, false)
	mainFlex.AddItem(helpView, 3, 1, false)

	// Set up key bindings
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			app.Stop()
			return nil
		case tcell.KeyTab:
			if buttonFlex.HasFocus() {
				app.SetFocus(peerDropdown)
			} else if peerDropdown.HasFocus() {
				app.SetFocus(logView)
			} else {
				app.SetFocus(buttonFlex)
			}
			return nil
		}
		return event
	})

	// Initialize with welcome message
	appendLog("Welcome to Hyperledger Fabric Test Network Control", "info")
	appendLog("Application started - See help section below for instructions", "system")

	if err := app.SetRoot(mainFlex, true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}
}
