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
}

type FTPUserConfig interface {
	GetUser(name string) FTPUser
}

func NewDefaultConfig(basedir string) FTPUserConfig {
	cfg := defaultUserConfiguration(basedir)
	return &cfg
}

type defaultUserConfiguration string

func (cfg *defaultUserConfiguration) GetUser(name string) FTPUser {
	return cfg
}

func (cfg *defaultUserConfiguration) HomeDir() string {
	return string(*cfg)
}

func (cfg *defaultUserConfiguration) Auth(password string) bool {
	return true
}

type yamlUserEntry struct {
	Home        string `yaml:"home"`
	Hash        string `yaml:"hash"`
	RawPassword string `yaml:"password"`
}

func (user *yamlUserEntry) HomeDir() string {
	return user.Home
}

func (user *yamlUserEntry) Auth(password string) bool {
	if err := bcrypt.CompareHashAndPassword([]byte(user.Hash), []byte(password)); err != nil {
		return false
	}
	return true
}

type yamlUserConfiguration struct {
	Users map[string]yamlUserEntry `yaml:"users"`
}

func (cfg *yamlUserConfiguration) GetUser(name string) FTPUser {
	user, ok := cfg.Users[name]
	if !ok {
		return nil
	}
	return &user
}

func NewYAMLConfig(file string, rewrite bool) (FTPUserConfig, error) {
	config := &yamlUserConfiguration{make(map[string]yamlUserEntry)}

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
