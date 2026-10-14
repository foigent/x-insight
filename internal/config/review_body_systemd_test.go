package config

import (
	"os"
	"syscall"
	"testing"
	"time"
)

type reviewBodyNativeInfo struct {
	mode os.FileMode
	stat syscall.Stat_t
}

func (i reviewBodyNativeInfo) Name() string       { return "review-body.key" }
func (i reviewBodyNativeInfo) Size() int64        { return 65 }
func (i reviewBodyNativeInfo) Mode() os.FileMode  { return i.mode }
func (i reviewBodyNativeInfo) ModTime() time.Time { return time.Time{} }
func (i reviewBodyNativeInfo) IsDir() bool        { return i.mode.IsDir() }
func (i reviewBodyNativeInfo) Sys() any           { return &i.stat }

func TestReviewBodyNativeSystemdCopyPreservesSingleReaderBoundary(t *testing.T) {
	for _, unit := range []string{"xinsight.service", "xinsight-source-facts.service"} {
		for _, filesystem := range []int64{0x858458f6, 0x01021994} {
			dir := "/run/credentials/" + unit
			info := reviewBodyNativeInfo{mode: 0400, stat: syscall.Stat_t{Uid: 1000, Gid: 0, Nlink: 1}}
			if !validReviewBodyNativeObservation(dir+"/review-body.key", info, 1000, dir, filesystem, true, true) {
				t.Fatal("native service-owned read-only credential rejected", unit, filesystem)
			}
		}
	}
	for _, kind := range []string{"root-process", "wrong-uid", "wrong-gid", "hardlink", "writable-file", "group-readable", "group-writable", "other-readable", "setuid", "setgid", "sticky", "symlink", "directory", "writable-mount", "ordinary-filesystem", "access-acl", "wrong-unit", "wrong-name", "wrong-directory", "unclean-path", "root-source"} {
		t.Run(kind, func(t *testing.T) {
			dir := "/run/credentials/xinsight.service"
			path, credentialsDir := dir+"/review-body.key", dir
			info := reviewBodyNativeInfo{mode: 0400, stat: syscall.Stat_t{Uid: 1000, Gid: 0, Nlink: 1}}
			euid, filesystem, readOnly, absent := 1000, int64(0x858458f6), true, true
			switch kind {
			case "root-process":
				euid, info.stat.Uid = 0, 0
			case "wrong-uid":
				info.stat.Uid = 1001
			case "wrong-gid":
				info.stat.Gid = 1000
			case "hardlink":
				info.stat.Nlink = 2
			case "writable-file":
				info.mode = 0600
			case "group-readable":
				info.mode = 0440
			case "group-writable":
				info.mode = 0420
			case "other-readable":
				info.mode = 0404
			case "setuid":
				info.mode |= os.ModeSetuid
			case "setgid":
				info.mode |= os.ModeSetgid
			case "sticky":
				info.mode |= os.ModeSticky
			case "symlink":
				info.mode |= os.ModeSymlink
			case "directory":
				info.mode |= os.ModeDir
			case "writable-mount":
				readOnly = false
			case "ordinary-filesystem":
				filesystem = 0xef53 // ext4, even if read-only, is not this native copy.
			case "access-acl":
				absent = false
			case "wrong-unit":
				path, credentialsDir = "/run/credentials/other.service/review-body.key", "/run/credentials/other.service"
			case "wrong-name":
				path = dir + "/source-profile-0.key"
			case "wrong-directory":
				credentialsDir = "/run/credentials/xinsight-source-facts.service"
			case "unclean-path":
				path = dir + "/../xinsight.service/review-body.key"
			case "root-source":
				path, info.mode, info.stat.Uid = "/etc/x-insight/review-body.key", 0600, 0
			}
			if validReviewBodyNativeObservation(path, info, euid, credentialsDir, filesystem, readOnly, absent) {
				t.Fatal("invalid native credential gained review-body authority")
			}
		})
	}
}
