package pullfeed

import (
	"github.com/foigent/x-insight/internal/testpostgres"
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(testpostgres.Main(m.Run)) }
