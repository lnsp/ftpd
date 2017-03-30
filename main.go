// Copyright 2017 Lennart Espe <lennart@espe.tech>
// All rights reserved.
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

	"github.com/lnsp/ftpd/config"
	"github.com/lnsp/ftpd/ftp"
)

const (
	modTimeFormat       = "20060102150405"
	defaultTransferType = "AN"
	transferBufferSize  = 4096
)

var (
	enableEPLF         = flag.Bool("eplf", false, "Enable EPLF (Easy parsed LIST Format)")
	serverPassiveBase  = flag.Int("base", 2122, "Change the passive port base number")
	serverPassiveRange = flag.Int("range", 1000, "Change the passive port range")
	serverPort         = flag.Int("port", 2121, "Change the public control port")
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

func HandleUser(conn ftp.FTPConnection) {
	defer conn.Close()
	ftp.SendResponse(conn, ftp.StatusServiceReady)
	for {
		rawRequest, err := conn.ReadCommand()
		if err != nil {
			return
		}
		cmdTokens := strings.Split(rawRequest, " ")
		if len(cmdTokens) < 1 {
			ftp.SendResponse(conn, ftp.StatusSyntaxError)
			continue
		}
		cmdName := strings.ToUpper(cmdTokens[0])
		cmdData := strings.Join(cmdTokens[1:], " ")

		conn.Log("REQUEST", cmdName, cmdData)

		switch cmdName {
		case ftp.CommandUser:
			ftp.SendResponse(conn, ftp.StatusAuthenticated)
		case ftp.CommandPassword:
			ftp.SendResponse(conn, ftp.StatusAuthenticated)
		case ftp.CommandSystemType:
			ftp.SendResponse(conn, ftp.StatusSystemType, *serverSystemName, encodeTransferType(defaultTransferType))
		case ftp.CommandPrintDirectory:
			ftp.SendResponse(conn, ftp.StatusWorkingDirectory, conn.GetDir())
		case ftp.CommandChangeDirectory:
			path, ok := conn.GetRelativePath(cmdData)
			if !ok {
				ftp.SendResponse(conn, ftp.StatusActionNotTaken)
				break
			}
			conn.ChangeDir(path)
			ftp.SendResponse(conn, ftp.StatusWorkingDirectory, conn.GetDir())
		case ftp.CommandDataType:
			encodedType := encodeTransferType(cmdData)
			if encodedType == "INVALID" {
				ftp.SendResponse(conn, ftp.StatusSyntaxParamError)
				break
			}
			conn.ChangeTransferType(cmdData)
			ftp.SendResponse(conn, ftp.StatusOK, "TYPE set to "+encodedType)
		case ftp.CommandModificationTime:
			path, ok := conn.GetRelativePath(cmdData)
			if !ok {
				ftp.SendResponse(conn, ftp.StatusActionNotTaken)
				break
			}
			info, err := os.Stat(path)
			if err != nil {
				ftp.SendResponse(conn, ftp.StatusActionNotTaken)
				break
			}
			ftp.SendResponse(conn, ftp.StatusFileInfo, info.ModTime().Format(modTimeFormat))
		case ftp.CommandFileSize:
			path, ok := conn.GetRelativePath(cmdData)
			if !ok {
				ftp.SendResponse(conn, ftp.StatusActionNotTaken)
				break
			}
			info, err := os.Stat(path)
			if err != nil {
				ftp.SendResponse(conn, ftp.StatusActionNotTaken)
				break
			}
			ftp.SendResponse(conn, ftp.StatusFileInfo, strconv.FormatInt(info.Size(), 10))
		case ftp.CommandRetrieveFile:
			path, ok := conn.GetRelativePath(cmdData)
			if !ok {
				ftp.SendResponse(conn, ftp.StatusActionNotTaken)
				break
			}
			buffer, err := ioutil.ReadFile(path)
			if err != nil {
				ftp.SendResponse(conn, ftp.StatusActionNotTaken)
				break
			}
			conn.Send(buffer)
		case ftp.CommandStoreFile:
			path, ok := conn.GetRelativePath(cmdData)
			if !ok {
				ftp.SendResponse(conn, ftp.StatusActionNotTaken)
				break
			}
			data, success := conn.Receive()
			if !success {
				break
			}
			if err := ioutil.WriteFile(path, data, 0644); err != nil {
				ftp.SendResponse(conn, ftp.StatusActionNotTaken)
				break
			}
		case ftp.CommandPassiveMode:
			passiveHost := *serverIP + ":" + strconv.Itoa(*serverPassiveBase+rand.Intn(*serverPassiveRange))
			conn.Reset()
			conn.SetPassive(passiveHost)
			ftp.SendResponse(conn, ftp.StatusPassiveMode, generateFTPHost(passiveHost))
		case ftp.CommandPort:
			conn.Reset()
			conn.SetActive(parseFTPHost(cmdData))
			ftp.SendResponse(conn, ftp.StatusOK, "PORT ftp.Command successfull")
		case ftp.CommandListRaw:
			cmd := exec.Command("/bin/ls", "-1", conn.GetDir())
			output, err := cmd.Output()
			if err != nil {
				ftp.SendResponse(conn, ftp.StatusLocalError)
				break
			}
			conn.Send(encodeText(output, conn.GetTransferType()))
		case ftp.CommandList:
			var buffer []byte
			if *enableEPLF {
				output, err := buildEPLFListing(conn.GetDir())
				if err != nil {
					ftp.SendResponse(conn, ftp.StatusLocalError)
					break
				}
				buffer = output
			} else {
				cmd := exec.Command("/bin/ls", "-l", conn.GetDir())
				output, err := cmd.Output()
				if err != nil {
					ftp.SendResponse(conn, ftp.StatusLocalError)
					break
				}
				buffer = encodeText(output, conn.GetTransferType())
			}
			conn.Send(buffer)
		case ftp.CommandQuit:
			ftp.SendResponse(conn, ftp.StatusOK, "Connection closing")
			return
		default:
			ftp.SendResponse(conn, ftp.StatusNotImplemented)
		}
	}
}

func encodeText(text []byte, mode string) []byte {
	return []byte(strings.Replace(string(text), "\n", "\r\n", -1))
}

func parseFTPHost(ports string) string {
	tokens := strings.Split(ports, ",")
	host := strings.Join(tokens[:4], ".")
	base1, _ := strconv.Atoi(tokens[4])
	base0, _ := strconv.Atoi(tokens[5])
	port := strconv.Itoa(base1*256 + base0)
	return host + ":" + port
}

func generateFTPHost(hostport string) string {
	tokens := strings.Split(hostport, ":")
	ips := strings.Split(tokens[0], ".")
	port, _ := strconv.Atoi(tokens[1])
	return fmt.Sprintf("%s,%d,%d", strings.Join(ips, ","), port/256, port%256)
}

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

func buildEPLFListing(dir string) ([]byte, error) {
	// This IS DIRTY. Does not work on Windows.
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

	factory := ftp.NewTCPConnectionFactory(*serverIP + ":" + strconv.Itoa(*serverPort))
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
		go HandleUser(conn)
	}
}
