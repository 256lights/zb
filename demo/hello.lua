return derivation {
  name = "hello";
  ["in"] = path "hello.txt";
  builder = "/bin/sh";
  system = "x86_64-linux";
  args = {"-c", "while read line; do echo \"$line\"; done < $in > $out"};
}
