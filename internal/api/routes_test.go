package api

import "testing"

// Two routes that both match a path make the server stop at start: every
// route is registered here first.
func TestRoutesRegister(t *testing.T) {
	defer func() {
		if failure := recover(); failure != nil {
			t.Fatalf("routes en conflit : %v", failure)
		}
	}()
	(&API{}).Handler()
}
