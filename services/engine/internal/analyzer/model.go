package analyzer

type RepositorySnapshot struct {
	Ref   string
	Files []File
}

type File struct {
	Path string
	Size int64
}

type Finding struct {
	Kind       string
	Value      string
	Evidence   []string
	Confidence float64
}

type Result struct {
	Version   int
	Ref       string
	Findings  []Finding
	Warnings  []string
}
