/*
Easy-to-setup FTP server with user auth, EPLF directory listings and support for passive / active mode.
*/
package main

import (
	"flag"
	"log"
	"runtime"
	"strconv"
	"time"

	"github.com/lnsp/ftpd/pkg/ftp/config"
	"github.com/lnsp/ftpd/pkg/ftp/handler"
	"github.com/lnsp/ftpd/pkg/ftp/tcp"
)

const (
	modTimeFormat       = "20060102150405"
	defaultTransferType = "AN"
	transferBufferSize  = 4096
	badLoginDelay       = 3 * time.Second
)

var (
	enableEPLF         = flag.Bool("eplf", false, "Enable EPLF (Easy parsed LIST Format)")
	serverPort         = flag.Int("port", 2121, "Change the public control port")
	serverMOTD         = flag.String("motd", "FTP Service ready", "Set the message of the day")
	serverIP           = flag.String("ip", "127.0.0.1", "Change the public IP")
	serverSystemName   = flag.String("system", runtime.GOOS, "Change the system name")
	serverUserConfig   = flag.String("config", "", "Enable a user configuration by file")
	serverUserConfigWb = flag.Bool("writeback", false, "Write back updated user configuration")
)

func main() {
	flag.Parse()

	cfg := config.NewDefaultConfig("/")
	if *serverUserConfig != "" {
		log.Println("LOADING CONFIG", *serverUserConfig, "WRITEBACK", *serverUserConfigWb)
		var err error
		cfg, err = config.NewYAMLConfig(*serverUserConfig, *serverUserConfigWb)
		if err != nil {
			log.Fatal(err)
		}
	}

	connHandler := handler.New(*serverIP, *serverSystemName, *serverMOTD, cfg, *enableEPLF)
	factory := tcp.NewFactory(*serverIP + ":" + strconv.Itoa(*serverPort))
	err := factory.Listen()
	if err != nil {
		log.Fatal(err)
	}
	log.Println("LISTENING ON", *serverIP+":"+strconv.Itoa(*serverPort))
	for {
		conn, err := factory.Accept(cfg)
		if err != nil {
			log.Print(err)
			continue
		}
		go connHandler.Handle(conn)
	}
}
