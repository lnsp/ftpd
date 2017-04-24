/*
Easy-to-setup FTP server with user auth, EPLF directory listings and support for passive / active mode.

Copyright 2017 Lennart Espe <lennart.espe@tech>. All rights reserved.
*/
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lnsp/ftpd/config"
	"github.com/lnsp/ftpd/ftp"
	"github.com/lnsp/ftpd/ftp/tcp"
)

const (
	modTimeFormat       = "20060102150405"
	defaultTransferType = "AN"
	transferBufferSize  = 4096
	badLoginDelay       = 3 * time.Second
)

var (
	enableEPLF         = flag.Bool("eplf", false, "Enable EPLF (Easy parsed LIST Format)")
	serverPassiveBase  = flag.Int("base", 2122, "Change the passive port base number")
	serverPassiveRange = flag.Int("range", 1000, "Change the passive port range")
	serverPort         = flag.Int("port", 2121, "Change the public control port")
	serverMOTD         = flag.String("motd", "FTP Service ready", "Set the message of the day")
	serverIP           = flag.String("ip", "127.0.0.1", "Change the public IP")
	serverSystemName   = flag.String("system", runtime.GOOS, "Change the system name")
	serverUserConfig   = flag.String("config", "", "Enable a user configuration by file")
	serverUserConfigWb = flag.Bool("writeback", false, "Write back updated user configuration")
	transferTypes      = map[rune]string{
		'A': "ASCII",
		'E': "EBCDIC",
		'I': "BINARY",
		'L': "LOCAL FORMAT",
		'N': "NON PRINT",
		'T': "TELNET",
		'C': "ASA CARRIAGE CONTROL",
	}
)

// HandleUser manages a FTP connection by interpreting commands and performing actions.
func HandleUser(conn ftp.Conn, cfg config.FTPUserConfig) {
	defer conn.Close()

	var selectedUser string
	conn.Respond(ftp.StatusServiceReady, *serverMOTD)
	for {
		rawRequest, err := conn.ReadCommand()
		if err != nil {
			return
		}
		cmdTokens := strings.Split(rawRequest, " ")
		if len(cmdTokens) < 1 {
			conn.Respond(ftp.StatusSyntaxError)
			continue
		}
		cmdName := strings.ToUpper(cmdTokens[0])
		cmdData := strings.Join(cmdTokens[1:], " ")

		conn.Log("REQUEST", cmdName, cmdData)

		if conn.GetUser() == "" && cmdName != ftp.CommandUser && cmdName != ftp.CommandPassword {
			conn.Respond(ftp.StatusNeedAccount)
			continue
		}

		switch cmdName {
		case ftp.CommandUser:
			if user := cfg.FindUser(cmdData); user != nil {
				conn.Respond(ftp.StatusNeedPassword)
				selectedUser = cmdData
			} else {
				conn.Respond(ftp.StatusNotLoggedIn)
			}
		case ftp.CommandPassword:
			if user := cfg.FindUser(selectedUser); user != nil {
				if user.Auth(cmdData) {
					conn.Respond(ftp.StatusAuthenticated)
					conn.ChangeUser(selectedUser)
					conn.ChangeDir(user.HomeDir())
					conn.Log("AUTH SUCCESS FOR USER", selectedUser)
				} else {
					conn.Log("AUTH FAILED FOR USER", selectedUser)
					time.Sleep(badLoginDelay)
					conn.Respond(ftp.StatusNotLoggedIn)
				}
			} else {
				conn.Respond(ftp.StatusNeedAccount)
			}
		case ftp.CommandSystemType:
			conn.Respond(ftp.StatusSystemType, *serverSystemName, encodeTransferType(defaultTransferType))
		case ftp.CommandPrintDirectory:
			user := cfg.FindUser(selectedUser)
			dir := conn.GetDir()
			if !user.Group().CanListDir(dir) {
				conn.Respond(ftp.StatusActionNotTaken)
				break
			}
			conn.Respond(ftp.StatusWorkingDirectory, dir)
		case ftp.CommandChangeDirectory:
			path, ok := conn.GetRelativePath(cmdData)
			if !ok {
				conn.Respond(ftp.StatusActionNotTaken)
				break
			}
			conn.ChangeDir(path)
			conn.Respond(ftp.StatusWorkingDirectory, conn.GetDir())
		case ftp.CommandDataType:
			encodedType := encodeTransferType(cmdData)
			if encodedType == "INVALID" {
				conn.Respond(ftp.StatusSyntaxParamError)
				break
			}
			conn.ChangeTransferType(cmdData)
			conn.Respond(ftp.StatusOK, "TYPE set to "+encodedType)
		case ftp.CommandModificationTime:
			path, ok := conn.GetRelativePath(cmdData)
			if !ok {
				conn.Respond(ftp.StatusActionNotTaken)
				break
			}
			info, err := os.Stat(path)
			if err != nil {
				conn.Respond(ftp.StatusActionNotTaken)
				break
			}
			conn.Respond(ftp.StatusFileInfo, info.ModTime().Format(modTimeFormat))
		case ftp.CommandFileSize:
			path, ok := conn.GetRelativePath(cmdData)
			if !ok {
				conn.Respond(ftp.StatusActionNotTaken)
				break
			}
			info, err := os.Stat(path)
			if err != nil {
				conn.Respond(ftp.StatusActionNotTaken)
				break
			}
			conn.Respond(ftp.StatusFileInfo, strconv.FormatInt(info.Size(), 10))
		case ftp.CommandRetrieveFile:
			path, ok := conn.GetRelativePath(cmdData)
			if !ok {
				conn.Respond(ftp.StatusActionNotTaken)
				break
			}
			buffer, err := ioutil.ReadFile(path)
			if err != nil {
				conn.Respond(ftp.StatusActionNotTaken)
				break
			}
			conn.Send(buffer)
		case ftp.CommandStoreFile:
			path, ok := conn.GetRelativePath(cmdData)
			if !ok {
				conn.Respond(ftp.StatusActionNotTaken)
				break
			}
			user := cfg.FindUser(selectedUser)
			if !user.Group().CanCreateFile(path) {
				conn.Respond(ftp.StatusActionNotTaken)
				break
			}
			data, success := conn.Receive()
			if !success {
				break
			}
			if err := ioutil.WriteFile(path, data, 0644); err != nil {
				conn.Respond(ftp.StatusActionNotTaken)
				break
			}
		case ftp.CommandPassiveMode:
			passiveHost := *serverIP + ":" + strconv.Itoa(*serverPassiveBase+rand.Intn(*serverPassiveRange))
			conn.Reset()
			conn.SetPassive(passiveHost)
			conn.Respond(ftp.StatusPassiveMode, ftp.GenerateHost(passiveHost))
		case ftp.CommandPort:
			conn.Reset()
			conn.SetActive(ftp.ParseHost(cmdData))
			conn.Respond(ftp.StatusOK, "PORT Command successfull")
		case ftp.CommandListRaw:
			user := cfg.FindUser(selectedUser)
			if !user.Group().CanListDir(conn.GetDir()) {
				conn.Respond(ftp.StatusActionNotTaken)
				break
			}
			cmd := exec.Command("/bin/ls", "-1", conn.GetDir())
			output, err := cmd.Output()
			if err != nil {
				conn.Log("ERROR", err, "WHILE RUNNING", cmd)
				conn.Respond(ftp.StatusLocalError)
				break
			}
			conn.Send(encodeText(output, conn.GetTransferType()))
		case ftp.CommandList:
			user := cfg.FindUser(selectedUser)
			if !user.Group().CanListDir(conn.GetDir()) {
				conn.Respond(ftp.StatusActionNotTaken)
				break
			}
			var buffer []byte
			if *enableEPLF {
				output, err := buildEPLFListing(conn.GetDir())
				if err != nil {
					conn.Log("ERROR", err, "WHILE RUNNING EPLF LISTING")
					conn.Respond(ftp.StatusLocalError)
					break
				}
				buffer = output
			} else {
				cmd := exec.Command("/bin/ls", "-l", conn.GetDir())
				output, err := cmd.Output()
				if err != nil {
					conn.Log("ERROR", err, "WHILE RUNNING", cmd)
					conn.Respond(ftp.StatusLocalError)
					break
				}
				buffer = encodeText(output, conn.GetTransferType())
			}
			conn.Send(buffer)
		case ftp.CommandQuit:
			conn.Respond(ftp.StatusOK, "Connection closing")
			return
		default:
			conn.Respond(ftp.StatusNotImplemented)
		}
	}
}

// encodeText converts strings with UNIX style lines to the FTP standard.
func encodeText(text []byte, mode string) []byte {
	return []byte(strings.Replace(string(text), "\n", "\r\n", -1))
}

// encodeTransferType generates a string representation of a transfer type code.
// e.g. "AN" -> "ASCII Non Print"
func encodeTransferType(tt string) string {
	var (
		baseMode, extMode string
		found             bool
		modeRunes         = []rune(tt)
	)
	if baseMode, found = transferTypes[modeRunes[0]]; !found {
		return "INVALID"
	}
	if len(modeRunes) == 2 {
		if extMode, found = transferTypes[modeRunes[1]]; !found {
			return "INVALID"
		}
	} else {
		extMode = transferTypes['N']
	}
	return fmt.Sprintf("%s %s", baseMode, extMode)
}

// buildEPLFListing generates a file listing.
func buildEPLFListing(dir string) ([]byte, error) {
	// This IS DIRTY. Does not work on Windows.
	// TODO: REWORK THIS METHOD
	output := ""
	directory, err := ioutil.ReadDir(dir)
	if err != nil {
		return []byte{}, err
	}
	for _, info := range directory {
		if !info.Mode().IsDir() && !info.Mode().IsRegular() {
			continue
		}
		output += "+"
		var stat syscall.Stat_t
		err = syscall.Stat(filepath.Join(dir, info.Name()), &stat)
		if err != nil {
			return []byte{}, err
		}
		output += "i" + strconv.FormatInt(int64(stat.Dev), 10) + "." + strconv.FormatUint(stat.Ino, 10) + ","
		output += "m" + strconv.FormatInt(info.ModTime().Unix(), 10) + ","
		if info.Mode().IsRegular() {
			output += "s" + strconv.FormatInt(info.Size(), 10) + ",r,"
		} else {
			output += "/,"
		}
		output += "\x09" + info.Name() + "\x0d\x0a"
	}
	return []byte(output), nil
}

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

	factory := tcp.NewFactory(*serverIP + ":" + strconv.Itoa(*serverPort))
	err := factory.Listen()
	if err != nil {
		log.Fatal(err)
	}

	for {
		conn, err := factory.Accept(cfg)
		if err != nil {
			log.Print(err)
			continue
		}
		go HandleUser(conn, cfg)
	}
}
