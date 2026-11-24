{
  description = "binary-checker - reports git-tracked binary files that should not be committed";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      lib = nixpkgs.lib;
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      version = self.shortRev or self.dirtyShortRev or "dev";

      forAllSystems = lib.genAttrs systems;

      source = lib.fileset.toSource {
        root = ./.;
        fileset = lib.fileset.unions [
          ./go.mod
          ./go.sum
          ./binary.go
          ./config.go
          ./doc.go
          ./git.go
          ./glob.go
          ./patterns.go
          ./rules.go
          ./scan.go
          ./cmd
          ./pkg
        ];
      };
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          go = pkgs.go_1_27;
        in
        {
          default = pkgs.buildGoModule.override { inherit go; } {
            pname = "binary-checker";
            inherit version;

            src = source;

            # Updated via `nix run .#update-vendor-hash` after dependency changes.
            vendorHash = "sha256-v/lROA2BuaJLACbHlUpSJUFbQoQ7ow115ycqm0SZyK0=";

            subPackages = [ "cmd/binary-checker" ];

            ldflags = [
              "-s"
              "-w"
              "-X main.version=${version}"
            ];

            doCheck = false;

            meta = {
              description = "Reports git-tracked binary files that should not be committed";
              homepage = "https://github.com/larsartmann/binary-checker";
              mainProgram = "binary-checker";
              license = lib.licenses.mit;
              platforms = lib.platforms.all;
            };
          };
        }
      );

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go_1_27
              git
              gopls
              gotools
              gofumpt
              golangci-lint
            ];

            shellHook = ''
              export GOEXPERIMENT=jsonv2
              export GOTOOLCHAIN=go1.27.1
            '';
          };
        }
      );

      checks = forAllSystems (system: {
        default = self.packages.${system}.default;
      });
    };
}
