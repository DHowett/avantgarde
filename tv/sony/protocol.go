package sony

import (
	"strings"
)

const (
	cmdPower            = `POWR`
	cmdVolume           = `VOLU`
	cmdMute             = `AMUT`
	cmdScreenMute       = `PMUT`
	cmdToggleScreenMute = `TPMU`
	cmdPIP              = `PIPI`
	cmdTogglePIP        = `TPIP`
	cmdInput            = `INPT`
	cmdChannel          = `CHNN`
	cmdMACAddress       = `MADR`
	cmdRemoteKey        = `IRCC`
)

const (
	typeEnquiry byte = 'E'
	typeCommand      = 'C'
	typeAnswer       = 'A'
	typeEvent        = 'N'
)

const responseValueError = `FFFFFFFFFFFFFFFF`
const requestValueNoData = `################`

func padRequestLeft(s string, c byte) string {
	if len(s) > 16 {
		s = s[:16]
	}
	return strings.Repeat(string(c), 16-len(s)) + s
}

func padRequestRight(s string, c byte) string {
	if len(s) > 16 {
		s = s[:16]
	}
	return s + strings.Repeat(string(c), 16-len(s))
}
