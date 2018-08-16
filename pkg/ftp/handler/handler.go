package handler

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lnsp/ftpd/pkg/ftp"
	"github.com/lnsp/ftpd/pkg/ftp/config"
)

var transferTypes = map[rune]string{
	'A': "ASCII",
	'E': "EBCDIC",
	'I': "BINARY",
	'L': "LOCAL FORMAT",
	'N': "NON PRINT",
	'T': "TELNET",
	'C': "ASA CARRIAGE CONTROL",
}

const (
	modTimeFormat       = "20060102150405"
	defaultTransferType = "AN"
	transferBufferSize  = 4096
	badLoginDelay       = 3 * time.Second
)

type HandleFunc func(*HandlerState, string)

func handleCommandUser(state *HandlerState, cmdData string) {
	if user := state.cfg.FindUser(cmdData); user != nil {
		state.conn.Respond(ftp.StatusNeedPassword)
		state.selectedUser = cmdData
	} else {
		state.conn.Respond(ftp.StatusNotLoggedIn)
	}
}

func handleCommandPassword(state *HandlerState, cmdData string) {
	if user := state.cfg.FindUser(state.selectedUser); user != nil {
		if user.Auth(cmdData) {
			state.conn.Respond(ftp.StatusAuthenticated)
			state.conn.ChangeUser(state.selectedUser)
			state.conn.ChangeDir(user.HomeDir())
			state.conn.Log("AUTH SUCCESS FOR USER", state.selectedUser)
		} else {
			state.conn.Log("AUTH FAILED FOR USER", state.selectedUser)
			time.Sleep(badLoginDelay)
			state.conn.Respond(ftp.StatusNotLoggedIn)
		}
	} else {
		state.conn.Respond(ftp.StatusNeedAccount)
	}
}

func handleCommandSystemType(state *HandlerState, cmdData string) {
	state.conn.Respond(ftp.StatusSystemType, state.src.SystemName, encodeTransferType(defaultTransferType))
}

func handleCommandPrintDirectory(state *HandlerState, cmdData string) {
	user := state.cfg.FindUser(state.selectedUser)
	dir := state.conn.GetDir()
	if !user.Group().CanListDir(dir) {
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
	state.conn.Respond(ftp.StatusWorkingDirectory, dir)
}

func handleCommandChangeDirectory(state *HandlerState, cmdData string) {
	path, ok := state.conn.GetRelativePath(cmdData)
	if !ok {
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
	state.conn.ChangeDir(path)
	state.conn.Respond(ftp.StatusWorkingDirectory, state.conn.GetDir())
}

func handleCommandDataType(state *HandlerState, cmdData string) {
	encodedType := encodeTransferType(cmdData)
	if encodedType == "INVALID" {
		state.conn.Respond(ftp.StatusSyntaxParamError)
		return
	}
	state.conn.ChangeTransferType(cmdData)
	state.conn.Respond(ftp.StatusOK, "TYPE set to "+encodedType)
}

func handleCommandModificationTime(state *HandlerState, cmdData string) {
	path, ok := state.conn.GetRelativePath(cmdData)
	if !ok {
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
	state.conn.Respond(ftp.StatusFileInfo, info.ModTime().Format(modTimeFormat))
}

func handleCommandFileSize(state *HandlerState, cmdData string) {
	path, ok := state.conn.GetRelativePath(cmdData)
	if !ok {
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
	state.conn.Respond(ftp.StatusFileInfo, strconv.FormatInt(info.Size(), 10))
}

func handleCommandRetrieveFile(state *HandlerState, cmdData string) {
	path, ok := state.conn.GetRelativePath(cmdData)
	if !ok {
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
	buffer, err := ioutil.ReadFile(path)
	if err != nil {
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
	state.conn.Send(buffer)
}

func handleCommandStoreFile(state *HandlerState, cmdData string) {
	path, ok := state.conn.GetRelativePath(cmdData)
	if !ok {
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
	user := state.cfg.FindUser(state.selectedUser)
	if !user.Group().CanCreateFile(path) {
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
	data, success := state.conn.Receive()
	if !success {
		return
	}
	if err := ioutil.WriteFile(path, data, 0644); err != nil {
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
}

func handleCommandPassiveMode(state *HandlerState, cmdData string) {
	state.conn.Reset()
	state.conn.SetPassive(state.src.PassiveServerHost)
	port, err := state.conn.GetPassivePort()
	if err != nil {
		state.conn.Reset()
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
	hostport := ftp.GenerateHost(fmt.Sprintf("%s:%d", state.src.PassiveServerHost, port))
	state.conn.Respond(ftp.StatusPassiveMode, hostport)
}

func handleCommandPort(state *HandlerState, cmdData string) {
	state.conn.Reset()
	state.conn.SetActive(ftp.ParseHost(cmdData))
	state.conn.Respond(ftp.StatusOK, "PORT Command successfull")
}

func handleCommandListRaw(state *HandlerState, cmdData string) {
	user := state.cfg.FindUser(state.selectedUser)
	if !user.Group().CanListDir(state.conn.GetDir()) {
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
	cmd := exec.Command("/bin/ls", "-1", state.conn.GetDir())
	output, err := cmd.Output()
	if err != nil {
		state.conn.Log("ERROR", err, "WHILE RUNNING", cmd)
		state.conn.Respond(ftp.StatusLocalError)
		return
	}
	state.conn.Send(encodeText(output, state.conn.GetTransferType()))
}

func handleCommandList(state *HandlerState, cmdData string) {
	user := state.cfg.FindUser(state.selectedUser)
	if !user.Group().CanListDir(state.conn.GetDir()) {
		state.conn.Respond(ftp.StatusActionNotTaken)
		return
	}
	var buffer []byte
	if state.src.EnableEPLF {
		output, err := buildEPLFListing(state.conn.GetDir())
		if err != nil {
			state.conn.Log("ERROR", err, "WHILE RUNNING EPLF LISTING")
			state.conn.Respond(ftp.StatusLocalError)
			return
		}
		buffer = output
	} else {
		cmd := exec.Command("/bin/ls", "-l", state.conn.GetDir())
		output, err := cmd.Output()
		if err != nil {
			state.conn.Log("ERROR", err, "WHILE RUNNING", cmd)
			state.conn.Respond(ftp.StatusLocalError)
			return
		}
		buffer = encodeText(output, state.conn.GetTransferType())
	}
	state.conn.Send(buffer)
}

func handleCommandQuit(state *HandlerState, cmdData string) {
	state.conn.Respond(ftp.StatusOK, "Connection closing")
}

var (
	defaultCommandHandlers = map[string]HandleFunc{
		ftp.CommandUser:             handleCommandUser,
		ftp.CommandPassword:         handleCommandPassword,
		ftp.CommandSystemType:       handleCommandSystemType,
		ftp.CommandPrintDirectory:   handleCommandPrintDirectory,
		ftp.CommandChangeDirectory:  handleCommandChangeDirectory,
		ftp.CommandDataType:         handleCommandDataType,
		ftp.CommandModificationTime: handleCommandModificationTime,
		ftp.CommandFileSize:         handleCommandFileSize,
		ftp.CommandRetrieveFile:     handleCommandRetrieveFile,
		ftp.CommandStoreFile:        handleCommandStoreFile,
		ftp.CommandPassiveMode:      handleCommandPassiveMode,
		ftp.CommandPort:             handleCommandPort,
		ftp.CommandListRaw:          handleCommandListRaw,
		ftp.CommandList:             handleCommandList,
		ftp.CommandQuit:             handleCommandQuit,
	}
)

func New(name, systemName, motd string, userCfg config.FTPUserConfig, enableEPLF bool) *Handler {
	return &Handler{
		EnableEPLF:        enableEPLF,
		PassiveServerHost: name,
		UserConfig:        userCfg,
		SystemName:        systemName,
		MOTD:              motd,
		cmdHandlers:       defaultCommandHandlers,
	}
}

type Handler struct {
	EnableEPLF        bool
	PassiveServerHost string
	SystemName        string
	MOTD              string
	UserConfig        config.FTPUserConfig
	cmdHandlers       map[string]HandleFunc
}

type HandlerState struct {
	src          *Handler
	conn         ftp.Conn
	cfg          config.FTPUserConfig
	keepAlive    bool
	selectedUser string
}

func (h *Handler) Handle(conn ftp.Conn) {
	defer conn.Close()

	state := &HandlerState{
		h, conn, h.UserConfig, true, "",
	}
	conn.Respond(ftp.StatusServiceReady, h.MOTD)
	for state.keepAlive {
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
		cmdHandler, ok := h.cmdHandlers[cmdName]
		if !ok {
			conn.Respond(ftp.StatusNotImplemented)
			continue
		}
		cmdHandler(state, cmdData)
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
	if runtime.GOOS == "windows" {
		panic("EPLF listing does not work on Windows")
	}
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
