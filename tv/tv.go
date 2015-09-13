package tv

import (
	"fmt"
	"io"
)

type Attribute uint

const (
	Power Attribute = 1 + iota
	Volume
	Mute
	OSD
	Input
	Tuning
	Screen
	Contrast
	Brightness
	Color
	Tint
	Sharpness
	Lock
	AudioBalance
	ColorTemperature
	Backlight
	Raw
)

type Operator uint

const (
	Set Operator = 1 + iota
	Increment
	Decrement
)

type Antenna uint

type Channel interface{}
type AnalogChannel uint
type DigitalChannel struct {
	Ch  uint
	Sub uint
}

type Tune struct {
	A Antenna
	C Channel
}

type Op struct {
	Attribute Attribute
	Operator  Operator
	Value     interface{}
}

type State struct{}

type Config interface {
	ModelSpecificRepresentation() interface{}
}

type TVModel interface {
	Initialize(io.ReadWriteCloser, Config) (TV, error)
	NewConfig() Config
}

type TV interface {
	Do(*Op) error
	State() (*State, error)
}

var tvModels = map[string]TVModel{}

func RegisterModel(name string, m TVModel) {
	tvModels[name] = m
}

func New(model string, rwc io.ReadWriteCloser, config Config) (TV, error) {
	tvm, ok := tvModels[model]
	if !ok {
		return nil, fmt.Errorf("tv: unknown model %s", model)
	}
	return tvm.Initialize(rwc, config)
}

func NewConfig(model string) Config {
	tvm, ok := tvModels[model]
	if !ok {
		panic(fmt.Errorf("tv: unknown model %s", model))
	}
	return tvm.NewConfig()
}
