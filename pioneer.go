// Package main provides ...
package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/distributed/sers"
	"github.com/jessevdk/go-flags"
	"gopkg.in/yaml.v2"

	"github.com/DHowett/avantgarde/tv"
	_ "github.com/DHowett/avantgarde/tv/lg"
)

var commandStream *CommandStream

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

func bindCommand(path string, o *tv.Op) {
	bindCommandGenerator(path, func(r *http.Request) *tv.Op {
		return o
	})
}

func bindCommandGenerator(path string, generator func(*http.Request) *tv.Op) {
	http.DefaultServeMux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		tvId, err := strconv.Atoi(r.FormValue("tv"))
		if err != nil {
			tvId = 0
		}

		if tvId >= len(tvs) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		cmd := generator(r)
		if cmd == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		err = tvs[tvId].Do(cmd)
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
		serialPort, err := sers.Open(tvc.V.Device)
		if err != nil {
			log.Fatalf("failed to open serial device `%v`: %v\n", tvc.V.Device, err.Error())
		}

		err = serialPort.SetMode(int(tvc.V.Baud), 8, sers.N, 1, sers.NO_HANDSHAKE)
		if err != nil {
			log.Fatalf("failed to configure serial device: %v\n", err.Error())
		}

		newTv, err := tv.New(tvc.V.Model, serialPort, tvc.ModelSpecific)
		if err != nil {
			log.Fatalf("failed to instantiate TV: %v\n", err.Error())
		}
		tvs = append(tvs, newTv)
	}

	quitC := make(chan struct{})

	/* Set up signal handling */
	sigChan := make(chan os.Signal)
	signal.Notify(sigChan, os.Interrupt, os.Kill)

	bindCommand("/tv/mute", &tv.Op{tv.Mute, tv.Set, true})
	bindCommand("/tv/unmute", &tv.Op{tv.Mute, tv.Set, false})
	bindCommandGenerator("/tv/power", boolGenerator("v", tv.Power))
	bindCommandGenerator("/tv/osd", boolGenerator("v", tv.OSD))
	bindCommandGenerator("/tv/volume", func(r *http.Request) *tv.Op {
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
	bindCommandGenerator("/tv/input", intGenerator("v", tv.Input))
	bindCommandGenerator("/tv/channel", func(r *http.Request) *tv.Op {
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
	bindCommandGenerator("/tv/raw", func(r *http.Request) *tv.Op {
		cmd := r.FormValue("v")
		if cmd == "" {
			return nil
		}
		return &tv.Op{tv.Raw, tv.Set, []byte(cmd)}
	})

	//commandStream.Run()

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
