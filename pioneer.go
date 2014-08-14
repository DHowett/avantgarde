// Package main provides ...
package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"

	"github.com/distributed/sers"
	"github.com/jessevdk/go-flags"
)

var commandStream *CommandStream

func bindCommand(path string, c Command) {
	bindCommandGenerator(path, func(r *http.Request) Command {
		return c
	})
}

func bindCommandGenerator(path string, generator func(*http.Request) Command) {
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

		cmd := generator(r)
		if cmd == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		err := <-commandStream.Submit(cmd)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

func boolGenerator(key string, cmd func(bool) Command) func(*http.Request) Command {
	return func(r *http.Request) Command {
		return cmd(r.FormValue(key) == "1")
	}
}
func intGenerator(key string, cmd func(int) Command) func(*http.Request) Command {
	return func(r *http.Request) Command {
		inp, e := strconv.Atoi(r.FormValue(key))
		if e != nil {
			return nil
		}
		return cmd(inp)
	}
}

type Options struct {
	Device string `short:"d" long:"dev" description:"serial device" default:"/dev/ttyUSB0"`
	Baud   int    `short:"b" long:"baud" description:"baud rate" default:"9600"`

	BindAddress string `short:"a" long:"addr" description:"bind address (web server)" default:":5456"`
}

func main() {
	quitC := make(chan struct{})

	/* Set up signal handling */
	sigChan := make(chan os.Signal)
	signal.Notify(sigChan, os.Interrupt, os.Kill)

	var opts Options
	if _, err := flags.Parse(&opts); err != nil {
		os.Exit(1)
	}

	serialPort, err := sers.Open(opts.Device)
	if err != nil {
		log.Fatalf("failed to open serial device `%v`: %v\n", opts.Device, err.Error())
	}

	err = serialPort.SetMode(opts.Baud, 8, sers.N, 1, sers.NO_HANDSHAKE)
	if err != nil {
		log.Fatalf("failed to configure serial device: %v\n", err.Error())
	}

	commandStream = NewCommandStream(NewCommandReadWriter(serialPort))

	bindCommand("/tv/mute", MuteCommand(true))
	bindCommand("/tv/unmute", MuteCommand(false))
	bindCommandGenerator("/tv/power", boolGenerator("v", PowerCommand))
	bindCommandGenerator("/tv/osd", boolGenerator("v", OSDCommand))
	bindCommandGenerator("/tv/volume", func(r *http.Request) Command {
		dir := r.FormValue("d")
		formV := r.FormValue("v")
		if formV == "max" {
			return VolumeMaxCommand()
		} else if formV == "min" {
			return VolumeMinCommand()
		}

		val, e := strconv.Atoi(formV)
		if e != nil {
			return nil
		}
		if dir == "up" {
			return VolumeUpCommand(val)
		} else if dir == "down" {
			return VolumeDownCommand(val)
		} else {
			return VolumeCommand(val)
		}
	})
	bindCommandGenerator("/tv/input", intGenerator("v", InputCommand))
	bindCommandGenerator("/tv/channel", func(r *http.Request) Command {
		ch, err := ParseChannel(r.FormValue("v"))
		if err != nil {
			return nil
		}
		antenna := r.FormValue("a")
		if antenna == "" {
			return nil
		}
		return TuneChannelCommand(Antenna(antenna), ch)
	})
	bindCommandGenerator("/tv/raw", func(r *http.Request) Command {
		cmd := r.FormValue("v")
		if cmd == "" {
			return nil
		}
		return StringCommand{cmd}
	})

	commandStream.Run()

	go func() {
		<-sigChan
		quitC <- struct{}{}
	}()

	go func() {
		http.ListenAndServe(opts.BindAddress, nil)
	}()

	<-quitC
	serialPort.Close()
}
