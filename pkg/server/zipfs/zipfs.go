package zipfs

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
)

type ZipFS struct {
	*zip.Reader
}

func NewZipFS(r *zip.Reader) *ZipFS {
	return &ZipFS{r}
}

func (z *ZipFS) Open(name string) (fs.File, error) {
	for _, f := range z.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			return &ZipFile{f, rc, 0}, nil
		}
	}
	return nil, os.ErrNotExist
}

type ZipFile struct {
	*zip.File
	rc     io.ReadCloser
	offset int64
}

func (f *ZipFile) Stat() (fs.FileInfo, error) {
	if f.File != nil {
		return f.FileInfo(), nil
	}
	return nil, fmt.Errorf("no file")
}

func (f *ZipFile) Close() error {
	if f.rc != nil {
		return f.rc.Close()
	}
	return nil
}

func (f *ZipFile) Read(b []byte) (int, error) {
	if f.rc == nil {
		var err error
		f.rc, err = f.Open()
		if err != nil {
			return 0, err
		}
	}
	n, err := f.rc.Read(b)
	f.offset += int64(n)
	if err == io.EOF {
		f.rc.Close()
		f.rc = nil
	}
	return n, err

}
func (f *ZipFile) Seek(offset int64, whence int) (int64, error) {
	if f.rc != nil {
		f.rc.Close()
		f.rc = nil
	}
	switch whence {
	case io.SeekStart:
		if offset < 0 {
			return 0, &fs.PathError{Op: "seek", Path: f.Name, Err: fmt.Errorf("negative position")}
		}
		f.offset = offset
	case io.SeekCurrent:
		if f.offset+offset < 0 {
			return 0, &fs.PathError{Op: "seek", Path: f.Name, Err: fmt.Errorf("negative position")}
		}
		f.offset += offset
	case io.SeekEnd:
		size := int64(f.UncompressedSize64)
		if size+offset < 0 {
			return 0, &fs.PathError{Op: "seek", Path: f.Name, Err: fmt.Errorf("negative position")}
		}
		f.offset = size + offset
	}
	return f.offset, nil
}

/*
func main() {
	r, err := zip.OpenReader("path/to/your.zip")
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	fs := NewZipFS(&r.Reader)
	file, err := fs.Open(path.Join("path", "to", "file"))
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Now you can use 'file' as a fs.File
}
*/
