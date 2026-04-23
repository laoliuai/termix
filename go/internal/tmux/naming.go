package tmux

func SessionName(sessionID string) string {
	return "termix_" + sessionID
}

func AttachCommand(sessionName string) string {
	return "tmux attach-session -t " + sessionName
}
