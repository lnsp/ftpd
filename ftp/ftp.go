package ftp

import (
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lnsp/ftpd/config"
)

/*
FTP Status Codes

100 - 199: Success, waiting for further commands

200 - 299: Successful execution

300 - 399: Success, waiting for further commands to conclude action

400 - 499: Command not executed, temporary problem

500 - 599: Command not executed, persistent problem
*/
const (
	StatusRestartMarker   = 110
	StatusServiceNotReady = 120
	StatusTransferReady   = 125
	StatusTransferStart   = 150

	StatusOK               = 200
	StatusNotImplementedOK = 202
	StatusSystemInfo       = 211
	StatusDirectoryInfo    = 212
	StatusFileInfo         = 213
	StatusHelpInfo         = 214
	StatusSystemType       = 215
	StatusServiceReady     = 220
	StatusCloseConnection  = 221
	StatusTransferOpen     = 225
	StatusTransferDone     = 226
	StatusPassiveMode      = 227
	StatusAuthenticated    = 230
	StatusActionDone       = 250
	StatusWorkingDirectory = 257

	StatusNeedPassword = 331
	StatusNeedAccount  = 332
	StatusNeedMoreInfo = 350

	StatusServiceUnavailable = 421
	StatusTransferFailed     = 425
	StatusTransferAbort      = 426
	StatusActionNotTaken     = 450
	StatusLocalError         = 451
	StatusInsufficientSpace  = 452

	StatusSyntaxError            = 500
	StatusSyntaxParamError       = 501
	StatusNotImplemented         = 502
	StatusBadSequence            = 503
	StatusNotImplementedParam    = 504
	StatusNotLoggedIn            = 530
	StatusStorageAccountRequired = 532
	StatusUnknownPage            = 551
	StatusInsufficientSpaceAbort = 552
	StatusInvalidName            = 553

	CommandQuit             = "QUIT"
	CommandUser             = "USER"
	CommandPassword         = "PASS"
	CommandSystemType       = "SYST"
	CommandPrintDirectory   = "PWD"
	CommandChangeDirectory  = "CWD"
	CommandModificationTime = "MDTM"
	CommandFileSize         = "SIZE"
	CommandStoreFile        = "STOR"
	CommandRetrieveFile     = "RETR"
	CommandDataType         = "TYPE"
	CommandPassiveMode      = "PASV"
	CommandPort             = "PORT"
	CommandListRaw          = "NLST"
	CommandList             = "LIST"
)

var (
	// StatusMessages maps status codes to response descriptions.
	StatusMessages = map[int]string{
		StatusRestartMarker:   "Restart marker reply",
		StatusServiceNotReady: "Service ready in %d minutes",
		StatusTransferReady:   "Data connection already open; transfer starting",
		StatusTransferStart:   "Opening data connection",

		StatusOK:               "%s",
		StatusNotImplementedOK: "Command not implemented",
		StatusSystemInfo:       "%s",
		StatusDirectoryInfo:    "%s",
		StatusFileInfo:         "%s",
		StatusHelpInfo:         "%s",
		StatusSystemType:       "%s Type: %s",
		StatusServiceReady:     "FTP Service ready",
		StatusCloseConnection:  "Service closing control connection",
		StatusTransferOpen:     "Data connection open; no transfer in progress",
		StatusTransferDone:     "Closing data connection",
		StatusPassiveMode:      "Entering Passive Mode (%s)",
		StatusAuthenticated:    "User logged in, proceed",
		StatusActionDone:       "Requested file action okay, completed",
		StatusWorkingDirectory: "\"%s\" is working directory.",

		StatusNeedPassword: "User name okay, need password",
		StatusNeedAccount:  "Need account for login",
		StatusNeedMoreInfo: "Requested file action pending further information",

		StatusServiceUnavailable: "Service not available, closing control connection",
		StatusTransferFailed:     "Can't open data connection",
		StatusTransferAbort:      "Connection closed; transfer aborted",
		StatusActionNotTaken:     "Requested file action not taken; file unavailable",
		StatusLocalError:         "Requested action aborted; local error in processing",
		StatusInsufficientSpace:  "Requested action not taken; insufficient storage space in system",

		StatusSyntaxError:            "Syntax error",
		StatusSyntaxParamError:       "Syntax error in parameters or arguments",
		StatusNotImplemented:         "Command not implemented",
		StatusBadSequence:            "Bad sequence of Commands",
		StatusNotImplementedParam:    "Command not implemented for that parameter",
		StatusNotLoggedIn:            "Not logged in",
		StatusStorageAccountRequired: "Need account for storing files",
		StatusUnknownPage:            "Requested action aborted; page type unknown",
		StatusInvalidName:            "Requested action not taken; file name not allowed",
	}
)

// Conn handles context related user interactions.
type Conn interface {
	Close()
	ReadCommand() (string, error)
	Write([]byte) error
	GetID() int
	GetRelativePath(string) (string, bool)
	GetDir() string
	ChangeDir(to string) bool
	GetUser() string
	ChangeUser(to string)
	GetTransferType() string
	ChangeTransferType(string)
	Send([]byte) bool
	Receive() ([]byte, bool)
	Log(...interface{})
	SetPassive(string)
	SetActive(string)
	Reset()
	Respond(int, ...interface{}) error
}

// ConnectionFactory waits for connections and matches them to a configuration.
type ConnectionFactory interface {
	Listen() error
	Accept(cfg config.FTPUserConfig) (Conn, error)
}

// ContextualConn stores FTP session information.
type ContextualConn struct {
	ID           int
	Dir          string
	User         string
	TransferType string
	Config       config.FTPUserConfig
}

// GetID retrieves the connection ID.
func (conn *ContextualConn) GetID() int {
	return conn.ID
}

// GetDir returns the current working directory.
func (conn *ContextualConn) GetDir() string {
	return conn.Dir
}

// GetUser returns the active user.
func (conn *ContextualConn) GetUser() string {
	return conn.User
}

// ChangeUser changes the user to the given name.
func (conn *ContextualConn) ChangeUser(to string) {
	conn.User = to
}

// ChangeDir changes the working directory to the target directory.
func (conn *ContextualConn) ChangeDir(dir string) bool {
	conn.Dir = dir
	return true
}

// GetTransferType retrieves the currently selected transfer type.
func (conn *ContextualConn) GetTransferType() string {
	return conn.TransferType
}

// ChangeTransferType selects a new transfer type.
func (conn *ContextualConn) ChangeTransferType(tt string) {
	conn.TransferType = tt
}

// Log prints out logging information including the connection ID.
func (conn *ContextualConn) Log(params ...interface{}) {
	log.Printf("[#%d] %s", conn.ID, fmt.Sprintln(params...))
}

// GetRelativePath returns the relative path from the current working directory to the target path.
func (conn *ContextualConn) GetRelativePath(p2 string) (string, bool) {
	p1 := conn.Dir
	if filepath.IsAbs(p2) {
		p1 = p2
	} else {
		p1 = filepath.Join(p1, p2)
	}
	p1, _ = filepath.Abs(p1)
	rel, err := filepath.Rel(conn.Config.FindUser(conn.User).HomeDir(), p1)
	if err != nil {
		return conn.Dir, false
	}
	if strings.Contains(rel, "..") {
		return conn.Dir, false
	}
	return p1, true
}

// ParseHost converts hostnames and ports between from the FTP to the URI format.
func ParseHost(ports string) string {
	tokens := strings.Split(ports, ",")
	host := strings.Join(tokens[:4], ".")
	base1, _ := strconv.Atoi(tokens[4])
	base0, _ := strconv.Atoi(tokens[5])
	port := strconv.Itoa(base1*256 + base0)
	return host + ":" + port
}

// GenerateHost converts a URI hostport to the FTP format.
func GenerateHost(hostport string) string {
	tokens := strings.Split(hostport, ":")
	ips := strings.Split(tokens[0], ".")
	port, _ := strconv.Atoi(tokens[1])
	return fmt.Sprintf("%s,%d,%d", strings.Join(ips, ","), port/256, port%256)
}
