// Package main provides ...
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/jessevdk/go-flags"
	"gopkg.in/yaml.v2"

	"github.com/DHowett/avantgarde/tv"
	_ "github.com/DHowett/avantgarde/tv/lg"
	_ "github.com/DHowett/avantgarde/tv/sony"
)

var inputNameToTV = map[string]tv.Connection{
	"coaxial": tv.Coaxial,
	"rf":      tv.Coaxial,
	"antenna": tv.Coaxial,
	"coax":    tv.Coaxial,

	"hdmi": tv.HDMI,
	"hd":   tv.HDMI,

	"scart": tv.SCART,

	"pc":  tv.PC,
	"rgb": tv.PC,

	"component": tv.Component,
	"ycbcr":     tv.Component,
	"ypbpr":     tv.Component,

	"composite": tv.Composite,
	"rca":       tv.Composite,

	"special": tv.Special,
}

var tvInputNames = []string{
	"coaxial", "component", "composite", "hdmi", "scart", "pc", "special",
}

func ParseChannel(s string) (tv.Channel, error) {
	ch, err := strconv.ParseUint(s, 10, 0)
	if err != nil { // This might be a digital channel
		parts := strings.Split(s, ".")
		if len(parts) != 2 {
			return nil, fmt.Errorf("%s: not an analog channel, but more or less than 2 components", s)
		}
		ch, err = strconv.ParseUint(parts[0], 10, 0)
		if err != nil { // This is not a channel :P
			return nil, fmt.Errorf("%s: failed to parse channel", s)
		}
		subch, err := strconv.ParseUint(parts[1], 10, 0)
		if err != nil { // This is not a channel :P
			return nil, fmt.Errorf("%s: failed to parse subchannel", s)
		}
		return tv.DigitalChannel{uint(ch), uint(subch)}, nil
	}
	return tv.AnalogChannel(uint(ch)), nil
}

type tvServer struct {
	reqTv map[*http.Request]int
	mux   *http.ServeMux
}

func newTVServer() *tvServer {
	sv := &tvServer{make(map[*http.Request]int), http.NewServeMux()}
	sv.mux.Handle("/status", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "OPTIONS, GET")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		tvId := sv.reqTv[r]
		state, err := tvs[tvId].State()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		enc := json.NewEncoder(w)
		err = enc.Encode(state)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
	return sv
}

func (sv *tvServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	comp := strings.SplitN(r.URL.Path, "/", 4)
	tvId, err := strconv.Atoi(comp[2])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if tvId >= len(tvs) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	r.URL.Path = "/" + strings.Join(comp[3:], "/")
	sv.reqTv[r] = tvId
	sv.mux.ServeHTTP(w, r)
	delete(sv.reqTv, r)
}

func (sv *tvServer) bindCommand(path string, o *tv.Op) {
	sv.bindCommandGenerator(path, func(r *http.Request) *tv.Op {
		return o
	})
}

func (sv *tvServer) bindCommandGenerator(path string, generator func(*http.Request) *tv.Op) {
	sv.mux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "OPTIONS, POST")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		tvId := sv.reqTv[r]

		cmd := generator(r)
		if cmd == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		err := tvs[tvId].Do(cmd)
		//err := <-commandStream.Submit(cmd)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

func boolGenerator(key string, attr tv.Attribute) func(*http.Request) *tv.Op {
	return func(r *http.Request) *tv.Op {
		return &tv.Op{attr, tv.Set, r.FormValue(key) == "1"}
	}
}
func intGenerator(key string, attr tv.Attribute) func(*http.Request) *tv.Op {
	return func(r *http.Request) *tv.Op {
		inp, e := strconv.Atoi(r.FormValue(key))
		if e != nil {
			return nil
		}
		return &tv.Op{attr, tv.Set, inp}
	}
}

type Port struct {
	Device string `yaml:"port"`
	Baud   uint   `yaml:"baud"`
}

type TVConfig struct {
	V struct {
		Name, Model string
		Port        `yaml:",inline"`
	}
	ModelSpecific tv.Config
}

func (tvc *TVConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	err := unmarshal(&tvc.V)
	if err != nil {
		return err
	}
	tvc.ModelSpecific = tv.NewConfig(tvc.V.Model)
	err = unmarshal(tvc.ModelSpecific)
	return err
}

type Config struct {
	TVs []TVConfig `yaml:"tvs"`
}

type Options struct {
	Config      string `short:"c" long:"config" description:"configuration file location" default:"./config.yml"`
	BindAddress string `short:"a" long:"addr" description:"bind address (web server)" default:":5456"`
}

var tvs []tv.TV

func main() {
	var opts Options
	if _, err := flags.Parse(&opts); err != nil {
		os.Exit(1)
	}

	cfgb, err := ioutil.ReadFile(opts.Config)
	if err != nil {
		panic(err)
	}

	var cfg Config
	err = yaml.Unmarshal(cfgb, &cfg)
	if err != nil {
		panic(err)
	}

	for _, tvc := range cfg.TVs {
		/*
			serialPort, err := sers.Open(tvc.V.Device)
			if err != nil {
				log.Fatalf("failed to open serial device `%v`: %v\n", tvc.V.Device, err.Error())
			}

			err = serialPort.SetMode(int(tvc.V.Baud), 8, sers.N, 1, sers.NO_HANDSHAKE)
			if err != nil {
				log.Fatalf("failed to configure serial device: %v\n", err.Error())
			}

		*/
		newTv, err := tv.New(tvc.V.Model, nil, tvc.ModelSpecific)
		if err != nil {
			log.Fatalf("failed to instantiate TV: %v\n", err.Error())
		}
		tvs = append(tvs, newTv)
	}

	quitC := make(chan struct{})

	/* Set up signal handling */
	sigChan := make(chan os.Signal)
	signal.Notify(sigChan, os.Interrupt, os.Kill)

	sv := newTVServer()
	sv.bindCommand("/mute", &tv.Op{tv.Mute, tv.Set, true})
	sv.bindCommand("/unmute", &tv.Op{tv.Mute, tv.Set, false})
	sv.bindCommandGenerator("/power", boolGenerator("v", tv.Power))
	sv.bindCommandGenerator("/screen", boolGenerator("v", tv.Screen))
	sv.bindCommandGenerator("/osd", boolGenerator("v", tv.OSD))
	sv.bindCommandGenerator("/volume", func(r *http.Request) *tv.Op {
		dir := r.FormValue("d")
		formV := r.FormValue("v")
		if formV == "max" {
			return &tv.Op{tv.Volume, tv.Set, 100}
		} else if formV == "min" {
			return &tv.Op{tv.Volume, tv.Set, 0}
		}

		val, e := strconv.Atoi(formV)
		if e != nil {
			return nil
		}
		if dir == "up" {
			return &tv.Op{tv.Volume, tv.Increment, 1}
		} else if dir == "down" {
			return &tv.Op{tv.Volume, tv.Decrement, 1}
		} else {
			return &tv.Op{tv.Volume, tv.Set, val}
		}
	})
	sv.bindCommandGenerator("/input", func(r *http.Request) *tv.Op {
		connectionName := r.FormValue("c")
		connectionNumberS := r.FormValue("n")
		if connectionName == "" || connectionNumberS == "" {
			return nil
		}
		connectionNumber, err := strconv.Atoi(connectionNumberS)
		if err != nil {
			return nil
		}

		connection, ok := inputNameToTV[connectionName]
		if !ok {
			return nil
		}
		return &tv.Op{tv.Input, tv.Set, tv.InputNumber{connection, connectionNumber}}

	})
	sv.bindCommandGenerator("/contrast", intGenerator("v", tv.Contrast))
	sv.bindCommandGenerator("/brightness", intGenerator("v", tv.Brightness))
	sv.bindCommandGenerator("/color", intGenerator("v", tv.Color))
	sv.bindCommandGenerator("/tint", intGenerator("v", tv.Tint))
	sv.bindCommandGenerator("/sharpness", intGenerator("v", tv.Sharpness))
	sv.bindCommandGenerator("/balance", intGenerator("v", tv.AudioBalance))
	sv.bindCommandGenerator("/color_temperature", intGenerator("v", tv.ColorTemperature))
	sv.bindCommandGenerator("/backlight", intGenerator("v", tv.Backlight))
	sv.bindCommandGenerator("/channel", func(r *http.Request) *tv.Op {
		ch, err := ParseChannel(r.FormValue("v"))
		if err != nil {
			return nil
		}
		/*
			antenna := r.FormValue("a")
			if antenna == "" {
				return nil
			}
		*/
		return &tv.Op{tv.Tuning, tv.Set, tv.Tune{0x01, ch}}
	})
	sv.bindCommandGenerator("/raw", func(r *http.Request) *tv.Op {
		cmd := r.FormValue("v")
		if cmd == "" {
			return nil
		}
		return &tv.Op{tv.Raw, tv.Set, []byte(cmd)}
	})

	//commandStream.Run()

	http.Handle("/tv/", sv)

	go func() {
		<-sigChan
		quitC <- struct{}{}
	}()

	go func() {
		http.ListenAndServe(opts.BindAddress, nil)
	}()

	<-quitC
	//serialPort.Close()
}
