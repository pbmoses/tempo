package friggdb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"
)

type IterFunc func(msg proto.Message) (bool, error)

// complete block has all of the fields
type completeBlock struct {
	meta     *blockMeta
	filepath string
	records  []*Record

	readFile *os.File
}

type ReplayBlock interface {
	Iterator(read proto.Message, fn IterFunc) error
	Identity() (blockID uuid.UUID, tenantID string, records []*Record, filepath string) // jpe : No more identity!
	Clear() error
}

type CompleteBlock interface {
	ReplayBlock

	Find(id ID, out proto.Message) (bool, error)
	Length() int
}

// todo:  I hate this method.  Make it not exist
func (c *completeBlock) Identity() (uuid.UUID, string, []*Record, string) {
	return c.meta.BlockID, c.meta.TenantID, c.records, c.fullFilename()
}

func (c *completeBlock) Find(id ID, out proto.Message) (bool, error) {

	i := sort.Search(len(c.records), func(idx int) bool {
		return bytes.Compare(c.records[idx].ID, id) >= 0
	})

	if i < 0 || i >= len(c.records) {
		return false, nil
	}

	rec := c.records[i]
	if bytes.Compare(rec.ID, id) != 0 {
		return false, nil
	}

	name := c.fullFilename()
	if c.readFile == nil {
		f, err := os.OpenFile(name, os.O_RDONLY, 0644)
		if err != nil {
			return false, err
		}
		c.readFile = f
	}

	b := make([]byte, rec.Length)
	_, err := c.readFile.ReadAt(b, int64(rec.Start))
	if err != nil {
		return false, err
	}

	err = proto.Unmarshal(b, out)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (c *completeBlock) Iterator(read proto.Message, fn IterFunc) error {
	name := c.fullFilename()
	f, err := os.OpenFile(name, os.O_RDONLY, 0644)
	defer f.Close()

	if err != nil {
		return err
	}

	for {
		var length uint32
		err := binary.Read(f, binary.LittleEndian, &length)
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}

		b := make([]byte, length)
		readLength, err := f.Read(b)
		if uint32(readLength) != length {
			return fmt.Errorf("read %d but expected %d", readLength, length)
		}

		err = proto.Unmarshal(b, read)
		if err != nil {
			return err
		}

		more, err := fn(read)
		if err != nil {
			return err
		}

		if !more {
			break
		}
	}

	return nil
}

func (c *completeBlock) Length() int {
	return len(c.records)
}

func (c *completeBlock) Clear() error {
	if c.readFile != nil {
		err := c.readFile.Close()
		if err != nil {
			return err
		}
	}

	name := c.fullFilename()
	return os.Remove(name)
}

func (c *completeBlock) fullFilename() string {
	return fmt.Sprintf("%s/%v:%v", c.filepath, c.meta.BlockID, c.meta.TenantID)
}
