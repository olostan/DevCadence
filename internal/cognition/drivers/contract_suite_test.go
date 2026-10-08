package drivers

import "testing"

func TestChannelEventStream_Concurrent(t *testing.T) {
	TestChannelEventStream_ConcurrentSendClose(t)
}
