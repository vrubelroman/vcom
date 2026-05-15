{
  description = "vcom terminal file manager";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        lib = pkgs.lib;
        packageBase = pkgs.buildGoModule {
          pname = "vcom";
          version = "0.2.6";
          src = ./.;
          vendorHash = null;

          subPackages = [ "cmd/vcom" ];

          ldflags = [
            "-s"
            "-w"
          ];

          meta = with lib; {
            description = "Terminal file manager inspired by Midnight Commander";
            homepage = "https://github.com/vrubelroman/vcom";
            license = licenses.mit;
            mainProgram = "vcom";
            platforms = platforms.linux;
          };
        };
        package = pkgs.symlinkJoin {
          name = "vcom";
          paths = [ packageBase ];
          nativeBuildInputs = [ pkgs.makeWrapper ];
          postBuild = ''
            wrapProgram "$out/bin/vcom" \
              --prefix PATH : "${lib.makeBinPath [ pkgs.ueberzugpp ]}"
          '';
        };
      in {
        packages.default = package;

        apps.default = {
          type = "app";
          program = "${package}/bin/vcom";
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
          ];
        };
      });
}
