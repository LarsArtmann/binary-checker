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
          (lib.fileset.fileFilter (file: lib.hasSuffix ".go" file.name) ./.)
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

            # No subPackages: the module has a single main package, and
            # buildGoModule only runs `go test` for subPackages when the list
            # is non-empty. Leaving it unset makes the check phase cover every
            # package, including the library.

            ldflags = [
              "-s"
              "-w"
              "-X main.version=${version}"
            ];

            # The suite shells out to git to build fixture repositories, so the
            # test phase needs git on PATH.
            nativeCheckInputs = [ pkgs.git ];
            doCheck = true;

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
              export GOTOOLCHAIN=local
            '';
          };
        }
      );

      checks = forAllSystems (system: {
        default = self.packages.${system}.default;
      });
    };
}
