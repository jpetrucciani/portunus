{ pkgs ? import
    (fetchTarball {
      name = "jpetrucciani-2024-11-07";
      url = "https://github.com/jpetrucciani/nix/archive/e20b804369c4c9ce8cb19f3c1982b72ac034701d.tar.gz";
      sha256 = "12nanbqxzlsjsrh9rszw1gm37g9ais3x8275v1ly23fpp9ci5vmc";
    })
    { }
}:
let
  name = "portunus";


  tools = with pkgs; {
    cli = [
      coreutils
      nixpkgs-fmt
    ];
    go = [
      go
      go-tools
      gopls
    ];
    scripts = pkgs.lib.attrsets.attrValues scripts;
  };

  scripts = with pkgs; { };
  paths = pkgs.lib.flatten [ (builtins.attrValues tools) ];
  env = pkgs.buildEnv {
    inherit name paths; buildInputs = paths;
  };
in
(env.overrideAttrs (_: {
  inherit name;
  NIXUP = "0.0.8";
})) // { inherit scripts; }
