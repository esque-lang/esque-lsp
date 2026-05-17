{
  description = "esque-lsp: Language Server Protocol implementation for the esque programming language";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      pkgsFor = system: nixpkgs.legacyPackages.${system};

      mkEsqueLsp = pkgs:
        pkgs.buildGoModule {
          pname = "esque-lsp";
          version = "0.1.0";

          src = pkgs.lib.cleanSourceWith {
            src = ./.;
            filter = path: type:
              let base = baseNameOf path; in
              base != "esque-lsp"
              && base != "result"
              && !(pkgs.lib.hasPrefix "result-" base);
          };

          # Pure stdlib — no third-party imports — so there is no vendor tree.
          vendorHash = null;

          ldflags = [ "-s" "-w" ];

          meta = with pkgs.lib; {
            description = "Language Server Protocol implementation for the esque programming language";
            homepage = "https://github.com/esque-lang/esque-lsp";
            license = licenses.mit;
            mainProgram = "esque-lsp";
            platforms = platforms.unix;
          };
        };
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = pkgsFor system;
          esque-lsp = mkEsqueLsp pkgs;
        in
        {
          default = esque-lsp;
          esque-lsp = esque-lsp;
        });

      overlays.default = final: _prev: {
        esque-lsp = mkEsqueLsp final;
      };

      devShells = forAllSystems (system:
        let pkgs = pkgsFor system; in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [ go gopls ];
          };
        });

      formatter = forAllSystems (system: (pkgsFor system).nixfmt);
    };
}
