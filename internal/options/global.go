package options

import (
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var DefaultConfigFlags = genericclioptions.NewConfigFlags(true).
	WithDeprecatedPasswordFlag().
	WithDiscoveryBurst(300).
	WithDiscoveryQPS(50.0)
