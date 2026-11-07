package pullrequests

import (
	"os"
	"testing"

	"github.com/sadmachine/asanamate/internal/gittest"
)

func TestMain(m *testing.M) {
	gittest.Isolate()
	os.Exit(m.Run())
}
