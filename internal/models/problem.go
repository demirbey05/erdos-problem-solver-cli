package models

// Problem represents a single Erdős problem from the YAML database.
type Problem struct {
	Number  string  `yaml:"number"`
	Prize   string  `yaml:"prize"`
	Status  Status  `yaml:"status"`
	OEIS    []string `yaml:"oeis"`
	Tags    []string `yaml:"tags"`
	Comments string  `yaml:"comments"`
}

// Status represents the resolution state of a problem.
type Status struct {
	State      string `yaml:"state"`
	LastUpdate string `yaml:"last_update"`
	Note       string `yaml:"note"`
}

// HasPrize returns true if a monetary prize is associated with this problem.
func (p Problem) HasPrize() bool {
	return p.Prize != "" && p.Prize != "no"
}

// IsOpen returns true if the problem is still open / unsolved.
func (p Problem) IsOpen() bool {
	return p.Status.State == "open"
}
