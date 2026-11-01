package config

// Version is onon's version. Release builds set it to the tag with
// -ldflags "-X onon/internal/config.Version=<tag>"; any other build is dev.
var Version = "dev"

// InstallMethod names the package manager onon was installed with, so that
// :check-updates can say how to upgrade. Packagers set it with
// -ldflags "-X onon/internal/config.InstallMethod=<name>". Empty means onon
// was downloaded and is upgraded by downloading the new release.
var InstallMethod = ""
