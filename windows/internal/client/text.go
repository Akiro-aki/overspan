package client

import "fmt"

// Every user-facing sentence in the viewer lives here, so wording stays
// consistent across screens and is easy to review.
const (
	TxtAppName  = "Overspan"
	TxtTagline  = "Use this PC's screen as another monitor."
	TxtTitle    = "Computers"
	TxtSearch   = "Looking for computers on this network…"
	TxtNoneHead = "No computers found yet"
	TxtNoneBody = "Start Overspan on your computer and keep both on the same network. You can also type the address shown on its dashboard."
	TxtAdd      = "Add by address"
	TxtRefresh  = "Search again"

	TxtReady  = "Ready"
	TxtPaused = "Paused"
	TxtSaved  = "Saved"

	TxtAddTitle   = "Add a computer"
	TxtAddLabel   = "Address shown on the computer"
	TxtAddHint    = "192.168.1.20:7940"
	TxtAddInvalid = "Type an address like 192.168.1.20 or 192.168.1.20:7940."
	TxtAddButton  = "Add"
	TxtCancel     = "Cancel"

	TxtConnect       = "Connect"
	TxtForget        = "Forget"
	TxtModeQuestion  = "How should this screen work?"
	TxtModeExtend    = "Extra monitor"
	TxtModeMirror    = "Same picture"
	TxtModeExtendTip = "Adds a new monitor next to the computer's screen."
	TxtModeMirrorTip = "Shows what is already on the computer's screen."
	TxtModeNoExtend  = "This computer's desktop can only share its existing screen."
	TxtViewQuestion  = "Open the picture"
	TxtViewFull      = "Full screen"
	TxtViewWindow    = "In a window"
	TxtDeviceName    = "Name for this PC"
	TxtDeviceTip     = "Shown on the computer when you connect."
	TxtChecking      = "Checking…"
	TxtUnreachable   = "Could not reach this computer. Check that Overspan is running there and that both are on the same network."
	TxtPausedText    = "This computer is not accepting devices right now. Turn it on from its dashboard."

	TxtConnecting    = "Connecting"
	TxtWaiting       = "Waiting for approval"
	TxtSetupExtend   = "Adding a monitor"
	TxtSetupExtendTx = "The computer is making room for this screen. This takes a few seconds."
	TxtSetupMirror   = "Starting the picture"
	TxtPINTitle      = "Enter the PIN"
	TxtPINText       = "This computer asks for a PIN before it shows its screen."
	TxtPINWrong      = "That PIN did not match. Try again."
	TxtPINLabel      = "PIN"
	TxtContinue      = "Continue"
	TxtLost          = "Connection lost"
	TxtLostText      = "The link to the computer dropped. Make sure both are on the same network."
	TxtReconnecting  = "Connection lost. Reconnecting…"
	TxtDenied        = "Connection declined"
	TxtDisabled      = "The computer is paused"
	TxtTimeout       = "No answer"
	TxtClosed        = "Disconnected"
	TxtNoDisplay     = "No display available"
	TxtNoPicture     = "Picture unavailable"
	TxtProtocol      = "Update needed"
	TxtRetry         = "Try again"
	TxtBack          = "Back"

	TxtTbFullscreen = "Full screen"
	TxtTbWindow     = "Leave full screen"
	TxtTbSettings   = "Settings"
	TxtTbDisconnect = "Disconnect"
	TxtHint         = "Move the pointer to the top edge, or press Alt+Shift+O, for controls."
	TxtInputOff     = "The computer has input from devices turned off."
	TxtInputOn      = "Input turned on"
	TxtAsleep       = "The computer's screen is asleep. Move the mouse or press a key to wake it."

	TxtSettings     = "Picture and input"
	TxtResolution   = "Resolution"
	TxtResTip       = "Lower is lighter on the network."
	TxtResNative    = "Sharpest"
	TxtResBalanced  = "Balanced"
	TxtResLight     = "Light"
	TxtFollow       = "Resize the monitor with this window"
	TxtStats        = "Show connection stats"
	TxtDone         = "Done"
	TxtWinKeys      = "The Windows key goes to the computer while the picture has focus. Ctrl+Alt+Del and Win+L stay with this PC."
	TxtDisconnectQ  = "Disconnect from %s?"
	TxtFoundOne     = "1 computer on this network"
	TxtFoundMany    = "%d computers on this network"
	TxtQualityLabel = "Image quality: %d"
	TxtFPSLabel     = "Frame rate limit: %d"
)

// FoundCount is the scan summary line.
func FoundCount(n int) string {
	switch n {
	case 0:
		return TxtSearch
	case 1:
		return TxtFoundOne
	}
	return fmt.Sprintf(TxtFoundMany, n)
}

// errorTitle maps a server error code to a screen title.
func errorTitle(code string) string {
	switch code {
	case "denied":
		return TxtDenied
	case "disabled":
		return TxtDisabled
	case "timeout":
		return TxtTimeout
	case "display":
		return TxtNoDisplay
	case "capture":
		return TxtNoPicture
	case "protocol":
		return TxtProtocol
	}
	return TxtClosed
}
