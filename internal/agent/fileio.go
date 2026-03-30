package agent

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	operationspb "github.com/beancrew/axon/gen/proto/operations"
)

const readChunkSize = 32 * 1024 // 32 KB

// FileIOHandler processes Read and Write requests on the local filesystem.
type FileIOHandler struct{}

// HandleRead stats the file, sends ReadMeta, then streams the content in
// readChunkSize chunks. Errors are reported as ReadOutput_Error messages.
func (h *FileIOHandler) HandleRead(req *operationspb.ReadRequest, send func(*operationspb.ReadOutput) error) {
	info, err := os.Stat(req.Path)
	if err != nil {
		send(readError(classifyError(err, req.Path))) //nolint:errcheck
		return
	}
	if info.IsDir() {
		send(readError(fmt.Sprintf("%s is a directory", req.Path))) //nolint:errcheck
		return
	}

	// Send metadata first.
	if err := send(&operationspb.ReadOutput{
		Payload: &operationspb.ReadOutput_Meta{
			Meta: &operationspb.ReadMeta{
				Size:       info.Size(),
				Mode:       int32(info.Mode().Perm()),
				ModifiedAt: info.ModTime().Unix(),
			},
		},
	}); err != nil {
		return
	}

	f, err := os.Open(req.Path)
	if err != nil {
		send(readError(classifyError(err, req.Path))) //nolint:errcheck
		return
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, readChunkSize)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if sendErr := send(&operationspb.ReadOutput{
				Payload: &operationspb.ReadOutput_Data{Data: chunk},
			}); sendErr != nil {
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				send(readError(fmt.Sprintf("read %s: %v", req.Path, err))) //nolint:errcheck
			}
			return
		}
	}
}

// HandleWrite receives a WriteHeader followed by data chunks, writes the
// content atomically (tmpfile + rename), and returns a WriteResponse.
func (h *FileIOHandler) HandleWrite(
	recvHeader func() (*operationspb.WriteHeader, error),
	recvData func() ([]byte, error),
) *operationspb.WriteResponse {
	hdr, err := recvHeader()
	if err != nil {
		return writeError(fmt.Sprintf("recv header: %v", err))
	}
	if hdr == nil {
		return writeError("first message must be WriteHeader")
	}

	path := hdr.Path
	mode := fs.FileMode(hdr.Mode)
	if mode == 0 {
		mode = 0644
	}

	// Create parent directories if needed.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return writeError(fmt.Sprintf("mkdir %s: %v", dir, err))
	}

	// Write to a temp file in the same directory for atomic rename.
	tmp, err := os.CreateTemp(dir, ".axon-write-*")
	if err != nil {
		return writeError(fmt.Sprintf("create temp: %v", err))
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		// Clean up temp file on failure.
		_ = os.Remove(tmpPath)
	}()

	var totalBytes int64
	for {
		data, err := recvData()
		if err != nil {
			if err == io.EOF {
				break
			}
			return writeError(fmt.Sprintf("recv data: %v", err))
		}
		if len(data) == 0 {
			// Empty chunk signals end of data (or just skip).
			break
		}
		n, err := tmp.Write(data)
		if err != nil {
			return writeError(fmt.Sprintf("write: %v", err))
		}
		totalBytes += int64(n)
	}

	// Set permissions before rename.
	if err := tmp.Chmod(mode); err != nil {
		return writeError(fmt.Sprintf("chmod: %v", err))
	}

	// Close before rename (required on some platforms).
	if err := tmp.Close(); err != nil {
		return writeError(fmt.Sprintf("close temp: %v", err))
	}

	// Atomic rename.
	if err := os.Rename(tmpPath, path); err != nil {
		return writeError(fmt.Sprintf("rename: %v", err))
	}

	return &operationspb.WriteResponse{
		Success:      true,
		BytesWritten: totalBytes,
	}
}

// classifyError returns a human-readable error message, detecting common
// filesystem errors like not-found and permission-denied.
func classifyError(err error, path string) string {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Sprintf("%s: not found", path)
	}
	if errors.Is(err, os.ErrPermission) {
		return fmt.Sprintf("%s: permission denied", path)
	}
	return fmt.Sprintf("%s: %v", path, err)
}

func readError(msg string) *operationspb.ReadOutput {
	return &operationspb.ReadOutput{
		Payload: &operationspb.ReadOutput_Error{Error: msg},
	}
}

func writeError(msg string) *operationspb.WriteResponse {
	return &operationspb.WriteResponse{
		Success: false,
		Error:   msg,
	}
}
