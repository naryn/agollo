package agollo

import (
	"encoding/json"
	"os"
)

// Conf ...
type Conf struct {
	AppID          string   `json:"appId,omitempty"`
	Cluster        string   `json:"cluster,omitempty"`
	NameSpaceNames []string `json:"namespaceNames,omitempty"`
	IP             string   `json:"ip,omitempty"`
	SecretKey      string   `json:"secretKey,omitempty"`
	Env            string   `json:"env,omitempty"`
	EncodeType     string   `json:"encodeType"`
	LocalFirst     bool     `json:"localFirst"`
	WatchClose     bool     `json:"watchClose"`
}

// NewConf create Conf from file
func NewConf(name string) (*Conf, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var ret Conf
	if err := json.NewDecoder(f).Decode(&ret); err != nil {
		return nil, err
	}

	return &ret, nil
}
