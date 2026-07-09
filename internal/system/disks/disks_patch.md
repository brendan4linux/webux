In internal/system/disks/disks.go, find the listBlockDevices() function.
Change the final loop from:

    var devs []BlockDevice
    for _, d := range raw.Blockdevices {
        devs = append(devs, toLsblk(d))
    }
    return devs, nil

To:

    var devs []BlockDevice
    for _, d := range raw.Blockdevices {
        bd := toLsblk(d)
        if !shouldShowDevice(bd) {
            continue
        }
        devs = append(devs, bd)
    }
    return devs, nil
