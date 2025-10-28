{ pkgs, self }:
pkgs.buildGoModule {
  pname = "beads";
  version = "0.17.7";

  src = self;

  # Point to the main Go package
  subPackages = [ "cmd/bd" ];

  # Go module dependencies hash (computed via nix build)
  # TODO: This hash needs to be updated after go.mod changes
  # The correct hash can be computed with: nix build --no-link --print-build-logs
  # Note: Upstream (steveyegge/beads) also has Nix test failures as of 0f5e92b
  vendorHash = "sha256-DJqTiLGLZNGhHXag50gHFXTVXCBdj8ytbYbPL3QAq8M=";

  meta = with pkgs.lib; {
    description = "beads (bd) - An issue tracker designed for AI-supervised coding workflows";
    homepage = "https://github.com/steveyegge/beads";
    license = licenses.mit;
    mainProgram = "bd";
    maintainers = [ ];
  };
}
