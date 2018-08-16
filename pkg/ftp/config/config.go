package config

import (
	"errors"
	"io/ioutil"

	"github.com/go-yaml/yaml"
	"golang.org/x/crypto/bcrypt"
)

type FTPUser interface {
	HomeDir() string
	Auth(password string) bool
	Group() FTPGroup
}

type FTPGroup interface {
	CanCreateFile(path string) bool
	CanCreateDir(path string) bool
	CanEditFile(path string) bool
	CanListDir(path string) bool
	CanDeleteFile(path string) bool
	CanDeleteDir(path string) bool
}

type FTPUserConfig interface {
	FindUser(name string) FTPUser
	FindGroup(name string) FTPGroup
}

func NewDefaultConfig(basedir string) FTPUserConfig {
	cfg := defaultUserConfiguration(basedir)
	return &cfg
}

type defaultUserConfiguration string

func (cfg *defaultUserConfiguration) FindGroup(name string) FTPGroup {
	return cfg
}

func (cfg *defaultUserConfiguration) FindUser(name string) FTPUser {
	return cfg
}

func (cfg *defaultUserConfiguration) HomeDir() string {
	return string(*cfg)
}

func (cfg *defaultUserConfiguration) Auth(password string) bool {
	return true
}

func (cfg *defaultUserConfiguration) Group() FTPGroup {
	return cfg
}

func (cfg *defaultUserConfiguration) CanCreateFile(path string) bool {
	return true
}

func (cfg *defaultUserConfiguration) CanEditFile(path string) bool {
	return true
}

func (cfg *defaultUserConfiguration) CanDeleteFile(path string) bool {
	return true
}

func (cfg *defaultUserConfiguration) CanCreateDir(path string) bool {
	return true
}

func (cfg *defaultUserConfiguration) CanListDir(path string) bool {
	return true
}

func (cfg *defaultUserConfiguration) CanDeleteDir(path string) bool {
	return true
}

type yamlUserEntry struct {
	Home        string `yaml:"home"`
	Hash        string `yaml:"hash"`
	RawPassword string `yaml:"password"`
	UserGroup   string `yaml:"group"`
	context     *yamlUserConfiguration
}

func (user *yamlUserEntry) HomeDir() string {
	return user.Home
}

func (user *yamlUserEntry) Auth(password string) bool {
	if user.Hash == "" && user.RawPassword == "" {
		return true
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Hash), []byte(password)); err != nil {
		return false
	}
	return true
}

func (user *yamlUserEntry) Group() FTPGroup {
	return user.context.FindGroup(user.UserGroup)
}

type yamlGroupEntry struct {
	CreateFlags []string `create`
	HandleFlags []string `handle`
	DeleteFlags []string `delete`
}

func (group *yamlGroupEntry) CanCreate(path string) bool {
	return true
}

func (group *yamlGroupEntry) CanHandle(path string) bool {
	return true
}

func (group *yamlGroupEntry) CanDelete(path string) bool {
	return true
}

type yamlUserConfiguration struct {
	Users  map[string]yamlUserEntry  `yaml:"users"`
	Groups map[string]yamlGroupEntry `yaml:"groups"`
}

func (cfg *yamlUserConfiguration) FindUser(name string) FTPUser {
	user, ok := cfg.Users[name]
	if !ok {
		return nil
	}
	user.context = cfg
	return &user
}

func (cfg *yamlUserConfiguration) FindGroup(name string) FTPGroup {
	group, ok := cfg.Groups[name]
	if !ok {
		return nil
	}
	return &group
}

func (group *yamlGroupEntry) CanCreateFile(path string) bool {
	for _, key := range group.CreateFlags {
		if key == "file" {
			return true
		}
	}
	return false
}

func (group *yamlGroupEntry) CanCreateDir(path string) bool {
	for _, key := range group.CreateFlags {
		if key == "dir" {
			return true
		}
	}
	return false
}

func (group *yamlGroupEntry) CanListDir(path string) bool {
	for _, key := range group.HandleFlags {
		if key == "dir" {
			return true
		}
	}
	return false
}

func (group *yamlGroupEntry) CanEditFile(path string) bool {
	for _, key := range group.HandleFlags {
		if key == "file" {
			return true
		}
	}
	return false
}

func (group *yamlGroupEntry) CanDeleteFile(path string) bool {
	for _, key := range group.DeleteFlags {
		if key == "file" {
			return true
		}
	}
	return false
}

func (group *yamlGroupEntry) CanDeleteDir(path string) bool {
	for _, key := range group.DeleteFlags {
		if key == "dir" {
			return true
		}
	}
	return false
}

func NewYAMLConfig(file string, rewrite bool) (FTPUserConfig, error) {
	config := &yamlUserConfiguration{make(map[string]yamlUserEntry), make(map[string]yamlGroupEntry)}

	buffer, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, errors.New("could not read config: " + err.Error())
	}
	if err := yaml.Unmarshal(buffer, config); err != nil {
		return nil, errors.New("could not unmarshal config: " + err.Error())
	}
	if !rewrite {
		return config, nil
	}

	for name, user := range config.Users {
		if user.RawPassword == "" {
			continue
		}
		hashed, err := bcrypt.GenerateFromPassword([]byte(user.RawPassword), bcrypt.DefaultCost)
		if err != nil {
			return nil, errors.New("could not generate password hash: " + err.Error())
		}
		user.RawPassword = ""
		user.Hash = string(hashed)
		config.Users[name] = user
	}

	buffer, err = yaml.Marshal(config)
	if err != nil {
		return nil, errors.New("could not marshal config: " + err.Error())
	}
	if err := ioutil.WriteFile(file, buffer, 0644); err != nil {
		return nil, errors.New("could not write back config: " + err.Error())
	}
	return config, nil
}
