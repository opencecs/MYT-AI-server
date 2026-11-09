package main

// iscsi_service_ssh.go 原始内容（757行）已按职责拆分到：
//   - iscsi_service_ssh_base.go     （sshIscsiadm/sshSudo/sshSudoLong/sshSudoLongChecked/devicePassword/shellEscape/shellArg/filterSudoOutput）
//   - iscsi_service_ssh_session.go  （discoverIscsiSSH/loginIscsiSSH/logoutIscsiSSH/listIscsiSSH/getIscsiSessionInfoSSH）
//   - iscsi_service_ssh_mount.go    （mountIscsiSSH/umountIscsiSSH/findIscsiBlockDeviceSSH）
//   - iscsi_service_ssh_disk.go     （autoInitDiskSSH/findExistingPartitionSSH/findFirstPartitionSSH/mountIscsiFilesystemSSH/umountIscsiFilesystemSSH）
//   - iscsi_service_ssh_migrate.go  （rsyncIfEmptySSH/rsyncBackToLocalSSH/migrateContainersSSH/getIscsiStatusSSH/resizeIscsiDeviceSSH）
//
// 拆分原则：仅移动代码块，保持 package main 与接收者不变，不改任何逻辑。
