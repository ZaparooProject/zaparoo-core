package installer

import (
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/cloudsoda/go-smb2"
	"github.com/rs/zerolog/log"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
)

func DownloadSMBFile(opts DownloaderArgs) error {
	u, err := url.Parse(opts.url)
	if err != nil {
		return fmt.Errorf("error parsing url: %w", err)
	}

	server := u.Host
	if _, _, err := net.SplitHostPort(server); err != nil {
		server = net.JoinHostPort(server, "445")
	}

	normalizedPath := strings.ReplaceAll(u.Path, "\\", "/")
	parts := strings.Split(strings.TrimPrefix(normalizedPath, "/"), "/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid SMB path format: %s", u.Path)
	}
	shareName := parts[0]
	filePath := strings.Join(parts[1:], "/")

	username := ""
	password := ""
	creds := config.LookupAuth(config.GetAuthCfg(), opts.url)
	if creds != nil {
		username = creds.Username
		password = creds.Password
	}

	d := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     username,
			Password: password,
		},
	}

	session, err := d.Dial(opts.ctx, server)
	if err != nil {
		return fmt.Errorf("error dialing SMB server: %w", err)
	}
	defer func(session *smb2.Session) {
		err := session.Logoff()
		if err != nil {
			log.Warn().Err(err).Msg("error logging off SMB session")
		}
	}(session)
	// TODO: on mister if this fails it the loader may get stuck

	fs, err := session.Mount(shareName)
	if err != nil {
		return fmt.Errorf("error mounting SMB share: %w", err)
	}
	defer func(fs *smb2.Share) {
		err := fs.Umount()
		if err != nil {
			log.Warn().Err(err).Msg("error unmounting SMB share")
		}
	}(fs)

	remoteFile, err := fs.Open(filePath)
	if err != nil {
		return fmt.Errorf("error opening SMB file: %w", err)
	}
	defer func(remoteFile *smb2.File) {
		err := remoteFile.Close()
		if err != nil {
			log.Warn().Err(err).Msg("error closing SMB file")
		}
	}(remoteFile)

	file, err := os.Create(opts.tempPath)
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}

	_, err = io.Copy(file, remoteFile)
	if err != nil {
		err = file.Close()
		if err != nil {
			log.Warn().Err(err).Msgf("error closing file: %s", opts.tempPath)
		}
		err := os.Remove(opts.tempPath)
		if err != nil {
			log.Warn().Err(err).Msgf("error removing partial download: %s", opts.tempPath)
		}
		return fmt.Errorf("error downloading file: %w", err)
	}

	err = file.Close()
	if err != nil {
		return fmt.Errorf("error closing file: %w", err)
	}

	if err := os.Rename(opts.tempPath, opts.finalPath); err != nil {
		err := os.Remove(opts.tempPath)
		if err != nil {
			log.Warn().Err(err).Msgf("error removing temp file: %s", opts.tempPath)
		}
		return fmt.Errorf("error renaming temp file: %w", err)
	}

	return nil
}
