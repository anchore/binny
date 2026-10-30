package internal

import "os"

// Symlink is how binny links store entries to their files. Tests replace it to act like a system that refuses
// symlinks, such as windows without developer mode.
var Symlink = os.Symlink
