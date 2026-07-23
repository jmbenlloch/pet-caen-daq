#include <hdf5.h>
#include <blosc_filter.h>

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define FILTER_BLOSC 32001

static int fail(const char *message, const char *path) {
    fprintf(stderr, "HDF5/Blosc verification failed: %s\n", message);
    if (path != NULL) {
        remove(path);
    }
    return EXIT_FAILURE;
}

int main(int argc, char **argv) {
    const char *path = argc == 2 ? argv[1] : "/tmp/pet-caen-hdf5-smoke.h5";
    const hsize_t dimensions[] = {8};
    const hsize_t chunk[] = {8};
    const uint32_t expected[] = {0, 1, 1, 2, 3, 5, 8, 13};
    uint32_t actual[8] = {0};
    unsigned int filter_options[] = {0, 0, 0, 0, 5, 1, 5};

    if (register_blosc(NULL, NULL) < 0 ||
        H5Zfilter_avail(FILTER_BLOSC) <= 0) {
        return fail("could not register Blosc filter 32001", path);
    }

    hid_t file = H5Fcreate(path, H5F_ACC_TRUNC, H5P_DEFAULT, H5P_DEFAULT);
    hid_t space = H5Screate_simple(1, dimensions, NULL);
    hid_t properties = H5Pcreate(H5P_DATASET_CREATE);
    if (file < 0 || space < 0 || properties < 0 ||
        H5Pset_chunk(properties, 1, chunk) < 0 ||
        H5Pset_filter(properties, FILTER_BLOSC, H5Z_FLAG_OPTIONAL,
                      sizeof(filter_options) / sizeof(filter_options[0]),
                      filter_options) < 0) {
        return fail("could not configure a Blosc dataset", path);
    }

    hid_t dataset = H5Dcreate2(file, "fibonacci", H5T_STD_U32LE, space,
                               H5P_DEFAULT, properties, H5P_DEFAULT);
    if (dataset < 0 ||
        H5Dwrite(dataset, H5T_NATIVE_UINT32, H5S_ALL, H5S_ALL, H5P_DEFAULT,
                 expected) < 0) {
        return fail("could not write a Blosc dataset", path);
    }
    H5Dclose(dataset);
    H5Pclose(properties);
    H5Sclose(space);
    H5Fclose(file);

    file = H5Fopen(path, H5F_ACC_RDONLY, H5P_DEFAULT);
    dataset = H5Dopen2(file, "fibonacci", H5P_DEFAULT);
    if (file < 0 || dataset < 0 ||
        H5Dread(dataset, H5T_NATIVE_UINT32, H5S_ALL, H5S_ALL, H5P_DEFAULT,
                actual) < 0 ||
        memcmp(expected, actual, sizeof(expected)) != 0) {
        return fail("Blosc round trip did not preserve values", path);
    }

    H5Dclose(dataset);
    H5Fclose(file);
    puts("HDF5 and Blosc write/read verification passed");
    return EXIT_SUCCESS;
}
