package rsync

// Options contains parameters for rsync execution.
type Options struct {
	Delete    bool
	Compress  bool
	DryRun    bool
	Archive   bool
	Verbose   bool
	Checksum  bool
	Partial   bool
	Bwlimit   string
	Excludes  []string
	Includes  []string
	ExtraArgs []string
}

// DefaultOptions returns standard rsync settings.
func DefaultOptions() Options {
	return Options{
		Delete:   false,
		Compress: true,
		DryRun:   false,
		Archive:  true,
		Verbose:  true,
	}
}

// BuildServerSenderCommand builds the wire-protocol command for the sending side of rsync.
func BuildServerSenderCommand(remotePath string, opts Options) []string {
	flags := "-logDtpr"
	if opts.Checksum {
		flags += "c"
	}
	if opts.Compress {
		flags += "z"
	}
	if opts.DryRun {
		flags += "n"
	}
	flags += "e.iLsfxC"

	cmd := []string{"rsync", "--server", "--sender"}
	for _, excl := range opts.Excludes {
		cmd = append(cmd, "--exclude="+excl)
	}
	for _, incl := range opts.Includes {
		cmd = append(cmd, "--include="+incl)
	}
	if opts.Bwlimit != "" {
		cmd = append(cmd, "--bwlimit="+opts.Bwlimit)
	}
	if opts.Partial {
		cmd = append(cmd, "--partial")
	}
	cmd = append(cmd, opts.ExtraArgs...)
	cmd = append(cmd, flags, ".", remotePath)
	return cmd
}

// BuildServerReceiverCommand builds the wire-protocol command for the receiving side of rsync.
func BuildServerReceiverCommand(remotePath string, opts Options) []string {
	flags := "-logDtpr"
	if opts.Checksum {
		flags += "c"
	}
	if opts.Compress {
		flags += "z"
	}
	if opts.DryRun {
		flags += "n"
	}
	flags += "e.iLsfxC"

	cmd := []string{"rsync", "--server"}
	if opts.Delete {
		cmd = append(cmd, "--delete")
	}
	for _, excl := range opts.Excludes {
		cmd = append(cmd, "--exclude="+excl)
	}
	for _, incl := range opts.Includes {
		cmd = append(cmd, "--include="+incl)
	}
	if opts.Bwlimit != "" {
		cmd = append(cmd, "--bwlimit="+opts.Bwlimit)
	}
	if opts.Partial {
		cmd = append(cmd, "--partial")
	}
	cmd = append(cmd, opts.ExtraArgs...)
	cmd = append(cmd, flags, ".", remotePath)
	return cmd
}
