package brick

// FmtConfig is the `fmt:` section of brick.yaml: the brick fmt options a
// site keeps with its files, so every editor and every `brick fmt` run in
// the team lays the files out the same way. Every option is off when the
// section or the key is absent, which is the layout brick fmt has always
// written. The brick fmt flags -line-length and -attr-order override the
// matching keys for one run.
type FmtConfig struct {
	// LineLength wraps a markup line longer than this many characters,
	// one attribute per line; 0 never wraps.
	LineLength int `yaml:"lineLength"`
	// AttrOrder lists the attributes moved to the front of each tag, in
	// this order; an entry ending in '*' names a prefix ("brick:*").
	AttrOrder []string `yaml:"attrOrder"`
	// Formatters maps brick:client, brick:style or brick:script to the
	// command that formats that tag's body: the body on standard input,
	// the result on standard output, run in the site's root. The command
	// is split on spaces and run without a shell, like a Taskfile's cmd
	// list; brick.yaml is the site's code, trusted like its templates.
	Formatters map[string]string `yaml:"formatters"`
	// Inline lists the elements and component tags whose content keeps
	// its parent's indentation (a, em, Icon), so prose wrapped inside them
	// is not pushed a level deeper; absent, every element but html, head
	// and body indents its content.
	Inline []string `yaml:"inline"`
}
