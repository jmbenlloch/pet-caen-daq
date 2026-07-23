//go:build hdf5

package hdf5store

/*
#include <hdf5.h>

static herr_t pet_caen_set_extent(long long id, hsize_t size) {
	hsize_t dimensions[1] = {size};
	return H5Dset_extent((hid_t)id, dimensions);
}
*/
import "C"

import (
	"fmt"

	hdf5 "github.com/next-exp/hdf5-go"
)

func setExtent(dataset *hdf5.Dataset, size uint64) error {
	if C.pet_caen_set_extent(C.longlong(dataset.ID()), C.hsize_t(size)) < 0 {
		return fmt.Errorf("extend dataset %q to %d rows", dataset.Name(), size)
	}
	return nil
}
