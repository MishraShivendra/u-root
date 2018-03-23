package boot

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"

	"github.com/u-root/u-root/pkg/cpio"
	"github.com/u-root/u-root/pkg/kexec"
	"github.com/u-root/u-root/pkg/uio"
)

// LinuxImage implements OSImage for a Linux kernel + initramfs.
type LinuxImage struct {
	Kernel  io.ReaderAt
	Initrd  io.ReaderAt
	Cmdline string
}

func newLinuxImageFromArchive(a *archive) (*LinuxImage, error) {
	kernel, ok := a.Files["modules/kernel/content"]
	if !ok {
		return nil, fmt.Errorf("kernel missing from archive")
	}

	li := &LinuxImage{}
	li.Kernel = kernel

	if params, ok := a.Files["modules/kernel/params"]; ok {
		b, err := ioutil.ReadAll(uio.Reader(params))
		if err != nil {
			return nil, err
		}
		li.Cmdline = string(b)
	}

	if initrd, ok := a.Files["modules/initrd/content"]; ok {
		li.Initrd = initrd
	}
	return li, nil
}

// Pack implements OSImage.Pack and writes all necessary files to the modules
// directory of `sw`.
func (li *LinuxImage) Pack(sw *SigningWriter) error {
	if err := sw.WriteRecord(cpio.Directory("modules", 0700)); err != nil {
		return err
	}
	if err := sw.WriteRecord(cpio.Directory("modules/kernel", 0700)); err != nil {
		return err
	}
	kernel, err := ioutil.ReadAll(uio.Reader(li.Kernel))
	if err != nil {
		return err
	}
	// TODO: avoid this unnecessary allocation.
	if err := sw.WriteFile("modules/kernel/content", string(kernel)); err != nil {
		return err
	}
	if err := sw.WriteFile("modules/kernel/params", li.Cmdline); err != nil {
		return err
	}

	if li.Initrd != nil {
		if err := sw.WriteRecord(cpio.Directory("modules/initrd", 0700)); err != nil {
			return err
		}
		initrd, err := ioutil.ReadAll(uio.Reader(li.Initrd))
		if err != nil {
			return err
		}
		if err := sw.WriteFile("modules/initrd/content", string(initrd)); err != nil {
			return err
		}
	}

	return sw.WriteFile("package_type", "linux")
}

func copyToFile(r io.Reader) (*os.File, error) {
	f, err := ioutil.TempFile("", "nerf-netboot")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}

	readOnlyF, err := os.Open(f.Name())
	if err != nil {
		return nil, err
	}
	return readOnlyF, nil
}

// ExecutionInfo implements OSImage.ExecutionInfo.
func (li *LinuxImage) ExecutionInfo(l *log.Logger) {
	k, err := copyToFile(uio.Reader(li.Kernel))
	if err != nil {
		l.Printf("Copying kernel to file: %v", err)
	}
	defer k.Close()

	var i *os.File
	if li.Initrd != nil {
		i, err = copyToFile(uio.Reader(li.Initrd))
		if err != nil {
			l.Printf("Copying initrd to file: %v", err)
		}
		defer i.Close()
	}

	l.Printf("Kernel: %s", k.Name())
	if i != nil {
		l.Printf("Initrd: %s", i.Name())
	}
	l.Printf("Command line: %s", li.Cmdline)
}

// Execute implements OSImage.Execute and kexec's the kernel with its initramfs.
func (li *LinuxImage) Execute() error {
	k, err := copyToFile(uio.Reader(li.Kernel))
	if err != nil {
		return err
	}
	defer k.Close()

	var i *os.File
	if li.Initrd != nil {
		i, err = copyToFile(uio.Reader(li.Initrd))
		if err != nil {
			return err
		}
		defer i.Close()
	}

	if err := kexec.FileLoad(k, i, li.Cmdline); err != nil {
		return err
	}
	return kexec.Reboot()
}