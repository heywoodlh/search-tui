{
  lib,
  buildGoModule,
  version ? "0.1.0",
}:

buildGoModule {
  pname = "tui-search";
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

  vendorHash = "sha256-FmRynMiZOJ4hh27gCPN4pZmjuodt4WJ8yzPQW2B42js=";

  subPackages = [ "cmd/search" ];

  env.CGO_ENABLED = 0;

  ldflags = [
    "-s"
    "-w"
    "-X github.com/heywoodlh/tui-search/internal/cli.Version=${version}"
  ];

  meta = {
    description = "Terminal UI and CLI for web and code search engines";
    homepage = "https://github.com/heywoodlh/tui-search";
    license = lib.licenses.gpl3Only;
    mainProgram = "search";
    platforms = lib.platforms.unix ++ lib.platforms.windows;
  };
}
