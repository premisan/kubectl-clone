package rsync

// Options contains parameters for rsync execution.
type Options struct {
	Delete   bool
	Compress bool
	DryRun   bool
	Archive  bool
	Verbose  bool
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
	if opts.Compress {
		flags += "z"
	}
	if opts.DryRun {
		flags += "n"
	}
	flags += "e.iLsfxC"

	return []string{"rsync", "--server", "--sender", flags, ".", remotePath}
}

// BuildServerReceiverCommand builds the wire-protocol command for the receiving side of rsync.
func BuildServerReceiverCommand(remotePath string, opts Options) []string {
	flags := "-logDtpr"
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
	cmd = append(cmd, flags, ".", remotePath)
	return cmd
}
