{
  lib,
  buildGoModule,
  version ? "0.1.0",
}:

buildGoModule {
  pname = "search-tui";
  inherit version;

  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../cmd
      ../internal
    ];
  };

  # Updated whenever go.mod/go.sum change (`nix build .#search-tui` reports the
  # expected hash on mismatch).
  vendorHash = "sha256-yMTioKPzxE1/sDfseLFbry3Q8OqzGBGg2c8UD8X9POM=";

  subPackages = [ "cmd/search" ];

  env.CGO_ENABLED = 0;

  ldflags = [
    "-s"
    "-w"
    "-X github.com/heywoodlh/search-tui/internal/cli.Version=${version}"
  ];

  meta = {
    description = "Terminal UI and CLI for web and code search engines";
    homepage = "https://github.com/heywoodlh/search-tui";
    license = lib.licenses.gpl3Only;
    mainProgram = "search";
    platforms = lib.platforms.unix ++ lib.platforms.windows;
  };
}
