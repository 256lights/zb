-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local archiveFile = path "archive.zip"

full = extract {
  src = archiveFile;
  stripFirstComponent = false;
}

stripped = extract {
  name = "foo.txt";
  src = archiveFile;
  stripFirstComponent = true;
}
