package config

import (
	"os"
	"syscall"
)

func reviewBodyCredentialDirectory(path string) string {
	for _, dir := range []string{"/run/credentials/xinsight.service", "/run/credentials/xinsight-source-facts.service"} {
		if path == dir+"/review-body.key" {
			return dir
		}
	}
	return ""
}

// A native systemd copy has the same single-reader boundary as its ACL form.
// The caller supplies observed filesystem/ACL facts from the path or opened FD;
// neither absence of evidence nor a writable plain file can authorize a copy.
func validReviewBodyNativeObservation(path string, info os.FileInfo, euid int, credentialsDirectory string, filesystemType int64, readOnly, accessACLAbsent bool) bool {
	dir := reviewBodyCredentialDirectory(path)
	stat, ok := info.Sys().(*syscall.Stat_t)
	return dir != "" && credentialsDirectory == dir && euid > 0 && ok &&
		info.Mode().IsRegular() && info.Mode().Perm() == 0400 &&
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 &&
		stat.Uid == uint32(euid) && stat.Gid == 0 && stat.Nlink == 1 &&
		(filesystemType == 0x858458f6 /* RAMFS_MAGIC */ || filesystemType == 0x01021994 /* TMPFS_MAGIC */) &&
		readOnly && accessACLAbsent
}
