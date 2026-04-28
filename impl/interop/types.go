package interop

type Service struct {
	Name         string `yaml:"name"`
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	Path         string `yaml:"path"`
	PreservePath bool   `yaml:"preservePath"`
	InjectCors   bool   `yaml:"injectCors"`
	Gone         bool   `yaml:"gone"`
	NoAuth       bool   `yaml:"noAuth"`
}

type CCInfo struct {
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	Endpoints map[string]string `json:"endpoints"`
}

type Entity struct {
	ID        string  `json:"ccid"`
	Alias     *string `json:"alias,omitempty"`
	Domain    string  `json:"domain"`
	TagString string  `json:"tag,omitempty"`
}
