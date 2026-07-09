package notify

import "context"

type Message struct {
	Title string
	Text  string
	URL   string
}

type Result struct {
	ChannelID string
	Status    string
	Error     string
	Retryable bool
}

type Notifier interface {
	Send(ctx context.Context, channelID string, msg Message) Result
}
