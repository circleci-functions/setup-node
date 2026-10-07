// Command setup-node installs Node.js and, through its install-packages
// subcommand, a project's dependencies with the right package manager, caching
// what each downloads.
//
// Supported platforms: linux, darwin, and windows.
package main

import (
	"context"

	"github.com/alecthomas/kong"

	"github.com/circleci-functions/toolkit/functionyaml"
	"github.com/circleci-functions/toolkit/sdk"
)

type cli struct {
	Install         installCmd         `cmd:"" default:"withargs" help:"Install Node.js and put node, npm and npx on PATH. Runs when no command is given."`
	InstallPackages installPackagesCmd `cmd:"" help:"Install the project's dependencies with its package manager, caching what it downloads."`
}

func main() {
	sdk.Run(run)
}

func run(ctx context.Context, client *sdk.Client) error {
	var flags cli
	kCtx := kong.Parse(&flags,
		kong.Name("setup-node"),
		kong.Description("Install Node.js, and optionally a project's dependencies, caching what each downloads."),
		kong.UsageOnError(),
		functionyaml.Embed(),
	)
	kCtx.BindTo(ctx, (*context.Context)(nil))
	kCtx.Bind(client)
	return kCtx.Run(ctx)
}
