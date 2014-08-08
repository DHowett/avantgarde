# Avant-garde

Pioneer televisions have an *integrator interface*, accessible via RS-232. This interface is a hardwired replacement for the infrared remote—it can change inputs, mute status, power, volume, channels, et-cetera.

**This is a web interface to the Pioneer integrator protocol.** It lives on port 5456, because `0x54 0x56` is `TV` in ASCII.

### `avantgarde` by example

```
# Turn the television on
curl 'http://localhost:5456/tv/power' -d 'v=1'
# Set the volume to 15 (15/100)
curl 'http://localhost:5456/tv/volume' -d 'v=15'
# Switch to Input 6
curl 'http://localhost:5456/tv/input' -d 'v=6'
```

### Options

```
Usage:
  avantgarde [OPTIONS]

Application Options:
  -d, --dev=  serial device (/dev/ttyUSB0)
  -b, --baud= baud rate (9600)
  -a, --addr= bind address (web server) (:5456)
```
