{
  description = "search: a terminal UI and CLI for web and code search engines";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs =
    { self, nixpkgs }:
    let
      inherit (nixpkgs) lib;
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAllSystems = f: lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = "0.1.0";
    in
    {
      packages = forAllSystems (pkgs: {
        tui-search = pkgs.callPackage ./nix/package.nix { inherit version; };
        default = self.packages.${pkgs.stdenv.hostPlatform.system}.tui-search;
      });

      apps = forAllSystems (pkgs: {
        default = {
          type = "app";
          program = lib.getExe self.packages.${pkgs.stdenv.hostPlatform.system}.default;
          meta.description = "Search the web and code from the terminal";
        };
      });

      overlays.default = final: _prev: {
        tui-search = final.callPackage ./nix/package.nix { inherit version; };
      };

      homeManagerModules.default = import ./nix/hm-module.nix self;
      homeModules.default = self.homeManagerModules.default;

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            gotools
          ];
        };
      });

      checks = forAllSystems (
        pkgs:
        let
          system = pkgs.stdenv.hostPlatform.system;
          # Evaluate the home-manager module without depending on home-manager:
          # stub the options it sets and check the generated TOML.
          hmEval = lib.evalModules {
            modules = [
              self.homeManagerModules.default
              {
                options = {
                  home.packages = lib.mkOption { type = lib.types.listOf lib.types.package; };
                  xdg.configFile = lib.mkOption { type = lib.types.attrsOf lib.types.anything; };
                };
                config.programs.tui-search = {
                  enable = true;
                  settings = {
                    default_engine = "wikipedia";
                    engines.github.token_command = "gh auth token";
                    keys.open = [ "o" ];
                  };
                };
              }
            ];
            specialArgs = { inherit pkgs; };
          };
          configFile = hmEval.config.xdg.configFile."tui-search/config.toml".source;
        in
        {
          package = self.packages.${system}.default;
          hm-module = pkgs.runCommand "tui-search-hm-module-check" { } ''
            grep -q 'default_engine = "wikipedia"' ${configFile}
            XDG_CONFIG_HOME=$TMPDIR/cfg
            mkdir -p $XDG_CONFIG_HOME/tui-search
            cp ${configFile} $XDG_CONFIG_HOME/tui-search/config.toml
            export XDG_CONFIG_HOME HOME=$TMPDIR
            ${lib.getExe self.packages.${system}.default} --list-engines | grep -q 'wikipedia (default)'
            touch $out
          '';
        }
      );

      formatter = forAllSystems (pkgs: pkgs.nixfmt);
    };
}
