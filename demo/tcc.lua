local stage0 = dofile("stage0/x86_64-linux.lua")

local version = "0.9.27";

local tarball = fetchurl {
  url = "https://download.savannah.gnu.org/releases/tinycc/tcc-"..version..".tar.bz2";
  hash = "sha256:177bdhwzrnqgyrdv1dwvpd04fcxj68s5pm1dzwny6359ziway8yy";
}

local src = derivation {
  name = "tcc-source-"..version;
  version = version;
  builder = stage0.stage0.."/bin/kaem";
  PATH = stage0.stage0.."/bin";
  tarball = tarball;
  args = {"-f", toFile("tcc-source-"..version.."-builder.sh", [[
mkdir ${TMPDIR}/${name}
unbz2 --file ${tarball} --output ${TMPDIR}/${name}/tcc.tar
mkdir ${out}
cd ${out}
untar --file ${TMPDIR}/${name}/tcc.tar
]])};
  system = "x86_64-linux";
}

-- TODO(now): Build!

return src
