package config

type Strategy string

const (
	StrategyPull    Strategy = "pull"
	StrategyPublish Strategy = "publish"
)

type VersionSourceType string

const (
	VersionSourceGit   VersionSourceType = "git"
	VersionSourceImage VersionSourceType = "image"
)

type Config struct {
	Version int     `yaml:"version"`
	Project Project `yaml:"project"`
	Update  Update  `yaml:"update"`
	Build   Build   `yaml:"build"`
	Images  []Image `yaml:"images"`
	Stores  Stores  `yaml:"stores"`
}

type Project struct {
	Root        string `yaml:"root"`
	BuildConfig string `yaml:"build_config"`
	PackageFile string `yaml:"package_file"`
	Output      string `yaml:"output"`
}

type Update struct {
	Strategy      Strategy      `yaml:"strategy"`
	VersionSource VersionSource `yaml:"version_source"`
}

type VersionSource struct {
	Type  VersionSourceType `yaml:"type"`
	Image string            `yaml:"image"`
}

type Build struct {
	Toolchains     []Toolchain `yaml:"toolchains"`
	RunBuildScript *bool       `yaml:"run_buildscript"`
}

type Toolchain struct {
	Kind    string `yaml:"kind"`
	Version string `yaml:"version"`
}

type Image struct {
	ID              string   `yaml:"id"`
	Target          string   `yaml:"target"`
	Service         string   `yaml:"service"`
	Source          string   `yaml:"source"`
	Channel         string   `yaml:"channel"`
	Sort            string   `yaml:"sort"`
	TagRegex        string   `yaml:"tag_regex"`
	ExcludeRegex    string   `yaml:"exclude_regex"`
	VersionRegex    string   `yaml:"version_regex"`
	VersionTemplate string   `yaml:"version_template"`
	Delivery        Delivery `yaml:"delivery"`
}

type Delivery struct {
	Mode               string `yaml:"mode"`
	ImageTemplate      string `yaml:"image_template"`
	RequireDigestMatch bool   `yaml:"require_digest_match"`
}

type Stores struct {
	Official OfficialStore `yaml:"official"`
	Private  PrivateStore  `yaml:"private"`
}

type OfficialStore struct {
	Enabled         bool                `yaml:"enabled"`
	CreateIfMissing bool                `yaml:"create_if_missing"`
	Locales         []string            `yaml:"changelog_locales"`
	Application     OfficialApplication `yaml:"application"`
}

type OfficialApplication struct {
	Language     string `yaml:"language"`
	Name         string `yaml:"name"`
	Source       string `yaml:"source"`
	SourceAuthor string `yaml:"source_author"`
}

type PrivateStore struct {
	Enabled bool   `yaml:"enabled"`
	Name    string `yaml:"name"`
	Summary string `yaml:"summary"`
}

func (build Build) ShouldRunBuildScript() bool {
	return build.RunBuildScript == nil || *build.RunBuildScript
}
