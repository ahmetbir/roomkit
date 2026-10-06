package room

import (
	"testing"

	"github.com/ahmetbir/roomkit/internal/tsconst"
)

// The client core's model of the session queue
// (ts/predict/sessionmodel.ts) has the server's sizes.
func TestQueueSizesMatchClientCore(t *testing.T) {
	for _, c := range []struct {
		name string
		want int
	}{{"QUEUE_CAP", queueCap}, {"QUEUE_KEEP", queueKeep}} {
		got, err := tsconst.Int("predict/sessionmodel.ts", c.name)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s: client %d, server %d", c.name, got, c.want)
		}
	}
}
