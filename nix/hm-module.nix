# Home Manager module for search-tui.
self:
{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.programs.search-tui;
  tomlFormat = pkgs.formats.toml { };
  inherit (lib)
    mkEnableOption
    mkIf
    mkOption
    literalExpression
    types
    ;
in
{
  options.programs.search-tui = {
    enable = mkEnableOption "search-tui, a terminal client for web and code search engines";

    package = mkOption {
      type = types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      defaultText = literalExpression "search-tui.packages.\${pkgs.stdenv.hostPlatform.system}.default";
      description = "The search-tui package to install.";
    };

    settings = mkOption {
      type = tomlFormat.type;
      default = { };
      description = ''
        Configuration written to {file}`$XDG_CONFIG_HOME/search-tui/config.toml`.
        Every key of the TOML config file is supported; run
        `search --example-config` for a documented example.

        Do not put secrets here (they would end up in the Nix store); use the
        `*_command`, `*_file` or `*_env` variants of `api_key`, `token`,
        `password` and `cx` instead.
      '';
      example = literalExpression ''
        {
          default_engine = "kagi";
          default_code_engine = "github";
          results = 15;
          browser = "firefox";
          low_power = false;
          summary.sentences = 4;
          ui = {
            layout = "auto";
            theme = "tokyo-night";
            colors.accent = "#7aa2f7";
          };
          keys = {
            open = [ "o" "enter" ];
            details = [ "l" "right" "space" ];
          };
          bangs = {
            nw = "https://wiki.nixos.org/w/index.php?search={query}";
          };
          engines = {
            kagi.token_file = "~/.config/kagi/session-token";
            github.token_command = "gh auth token";
            work = {
              type = "gitlab";
              base_url = "https://gitlab.example.com";
              token_command = "pass show work/gitlab";
              bangs = [ "work" ];
            };
          };
        }
      '';
    };
  };

  config = mkIf cfg.enable {
    home.packages = [ cfg.package ];

    xdg.configFile."search-tui/config.toml" = mkIf (cfg.settings != { }) {
      source = tomlFormat.generate "search-tui-config.toml" cfg.settings;
    };
  };
}
