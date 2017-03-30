package ftp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lnsp/ftpd/config"
)

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

type FTPConnection interface {
	Close()
	ReadCommand() (string, error)
	Write([]byte) error
	GetID() int
	GetRelativePath(string) (string, bool)
	GetDir() string
	ChangeDir(to string) bool
	GetUser() string
	GetTransferType() string
	ChangeTransferType(string)
	Send([]byte) bool
	Receive() ([]byte, bool)
	Log(...interface{})
	SetPassive(string)
	SetActive(string)
	Reset()
}

type FTPConnectionFactory interface {
	Listen() error
	Accept(cfg config.FTPUserConfig) (FTPConnection, error)
}

func SendResponse(conn FTPConnection, status int, params ...interface{}) error {
	response := fmt.Sprintf("%d %s\r\n", status, fmt.Sprintf(StatusMessages[status], params...))
	err := conn.Write([]byte(response))
	if err != nil {
		return err
	}
	conn.Log("RESPONSE", strings.TrimSpace(response))
	return nil
}

func ParseHost(ports string) string {
	tokens := strings.Split(ports, ",")
	host := strings.Join(tokens[:4], ".")
	base1, _ := strconv.Atoi(tokens[4])
	base0, _ := strconv.Atoi(tokens[5])
	port := strconv.Itoa(base1*256 + base0)
	return host + ":" + port
}

func GenerateHost(hostport string) string {
	tokens := strings.Split(hostport, ":")
	ips := strings.Split(tokens[0], ".")
	port, _ := strconv.Atoi(tokens[1])
	return fmt.Sprintf("%s,%d,%d", strings.Join(ips, ","), port/256, port%256)
}
