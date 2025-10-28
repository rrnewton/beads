{ pkgs, self }:
pkgs.buildGoModule {
  pname = "beads";
  version = "0.17.7";

  src = self;

  # Point to the main Go package
  subPackages = [ "cmd/bd" ];

  # Go module dependencies hash (computed via nix build)
  # Using lib.fakeHash to trigger Nix to tell us the correct hash
  vendorHash = pkgs.lib.fakeHash;

  meta = with pkgs.lib; {
    description = "beads (bd) - An issue tracker designed for AI-supervised coding workflows";
    homepage = "https://github.com/steveyegge/beads";
    license = licenses.mit;
    mainProgram = "bd";
    maintainers = [ ];
  };
}
