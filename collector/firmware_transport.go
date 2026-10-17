package collector

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/alecthomas/kingpin/v2"
	"github.com/davidcollom/rpi_exporter/collector/firmware"
)

var firmwareTransport = kingpin.Flag(
	"firmware.transport",
	"Firmware transport: exec uses host vcgencmd; mailbox uses /dev/vcio directly (experimental).",
).Default("exec").Enum("exec", "mailbox")

func firmwareCommand(transport, executable, cmd string) ([]byte, error) {
	switch transport {
	case "mailbox":
		client, err := firmware.Open()
		if err != nil {
			return nil, err
		}
		defer client.Close()
		output, err := client.Run(cmd)
		if err != nil {
			return nil, fmt.Errorf("firmware %q: %w", cmd, err)
		}
		return []byte(output), nil
	case "exec":
		return exec.Command(executable, strings.Fields(cmd)...).Output()
	default:
		return nil, fmt.Errorf("unsupported firmware transport: %s", transport)
	}
}
