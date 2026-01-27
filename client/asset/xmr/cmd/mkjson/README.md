# MkJson

## A tool for the download and JSON conversion of the github information about the most recent few versions of Monero CLI.

- The JSON can be used as input to Monero Tools downloading in the `toolsdl` package.
- If the pattern of the Monero github page(s) changes this tool should be changed

## Usage

Linux/Mac: 

1.Build

`$ go build`

2.Run

`$ ./mkjson`

## More Information

This tool will download the first releases page from <https://github.com/decred/dcrdex/releases> and then pull out the versioned Hashed Zip lines like:-


```text
monero-win-x64-v0.18.4.3.zip, bd9f615657c35d2d7dd9a5168ad54f1547dbf9a335dee7f12fab115f6f394e36
monero-win-x86-v0.18.4.3.zip, e642ed7bbfa34c30b185387fa553aa9c3ea608db1f3fc0e9332afa9b522c9c1a
monero-mac-x64-v0.18.4.3.tar.bz2, a8d8273b14f31569f5b7aa3063fbd322e3caec3d63f9f51e287dfc539c7f7d61
monero-mac-armv8-v0.18.4.3.tar.bz2, bab9a6d3c2ca519386cff5ff0b5601642a495ed1a209736acaf354468cba1145
monero-linux-x64-v0.18.4.3.tar.bz2, 3a7b36ae4da831a4e9913e0a891728f4c43cd320f9b136cdb6686b1d0a33fafa
monero-linux-x86-v0.18.4.3.tar.bz2, e0b51ca71934c33cb83cfa8535ffffebf431a2fc9efe3acf2baad96fb6ce21ec
monero-linux-armv8-v0.18.4.3.tar.bz2, b1cc5f135de3ba8512d56deb4b536b38c41addde922b2a53bf443aeaf2a5a800
monero-linux-armv7-v0.18.4.3.tar.bz2, 3ac83049bc565fb5238501f0fa629cdd473bbe94d5fb815088af8e6ff1d761cd
monero-linux-riscv64-v0.18.4.3.tar.bz2 95baaa6e8957b92caeaed7fb19b5c2659373df8dd5f4de2601ed3dae7b17ce2f
monero-android-armv8-v0.18.4.3.tar.bz2, 1aebd24aaaec3d1e87a64163f2e30ab2cd45f3902a7a859413f6870944775c21
monero-android-armv7-v0.18.4.3.tar.bz2, 4e1481835824b9233f204553d4a19645274824f3f6185d8a4b50198470752f54
monero-freebsd-x64-v0.18.4.3.tar.bz2, ff7b9c5cf2cb3d602c3dff1902ac0bc3394768cefc260b6003a9ad4bcfb7c6a4
```

... then process and emit all the versions to STDOUT as JSON like this:-

```text
{
  "vers": [
    {
      "zips": [
        {
          "hash": "7eb3b87a105b3711361dd2b3e492ad14219d21ed8fd3dd726573a6cbd96e83a6",
          "zip": "monero-win-x64-v0.18.4.4.zip",
          "dir": "monero-win-x64-v0.18.4.4",
          "ext": "zip",
          "os": "win",
          "arch": "x64"
        },
        {
          "hash": "a148a2bd2b14183fb36e2cf917fce6f33fb687564db2ed53193b8432097ab398",
          "zip": "monero-win-x86-v0.18.4.4.zip",
          "dir": "monero-win-x86-v0.18.4.4",
          "ext": "zip",
          "os": "win",
          "arch": "x86"
        },
        {
          "hash": "af3d98f09da94632db3e2f53c62cc612e70bf94aa5942d2a5200b4393cd9c842",
          "zip": "monero-mac-x64-v0.18.4.4.tar.bz2",
          "dir": "monero-mac-x64-v0.18.4.4",
          "ext": "tar.bz2",
          "os": "mac",
          "arch": "x64"
        },
        {
          "hash": "645e9bbae0275f555b2d72a9aa30d5f382df787ca9528d531521750ce2da9768",
          "zip": "monero-mac-armv8-v0.18.4.4.tar.bz2",
          "dir": "monero-mac-armv8-v0.18.4.4",
          "ext": "tar.bz2",
          "os": "mac",
          "arch": "armv8"
        },
        {
          "hash": "7fe45ee9aade429ccdcfcad93b905ba45da5d3b46d2dc8c6d5afc48bd9e7f108",
          "zip": "monero-linux-x64-v0.18.4.4.tar.bz2",
          "dir": "monero-linux-x64-v0.18.4.4",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "x64"
        },
        {
          "hash": "8c174b756e104534f3d3a69fe68af66d6dc4d66afa97dfe31735f8d069d20570",
          "zip": "monero-linux-x86-v0.18.4.4.tar.bz2",
          "dir": "monero-linux-x86-v0.18.4.4",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "x86"
        },
        {
          "hash": "b9daede195a24bdd05bba68cb5cb21e42c2e18b82d4d134850408078a44231c5",
          "zip": "monero-linux-armv8-v0.18.4.4.tar.bz2",
          "dir": "monero-linux-armv8-v0.18.4.4",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "armv8"
        },
        {
          "hash": "2040dc22748ef39ed8a755324d2515261b65315c67b91f449fa1617c5978910b",
          "zip": "monero-linux-armv7-v0.18.4.4.tar.bz2",
          "dir": "monero-linux-armv7-v0.18.4.4",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "armv7"
        },
        {
          "hash": "c939ea6e8002798f24a56ac03cbfc4ff586f70d7d9c3321b7794b3bcd1fa4c45",
          "zip": "monero-linux-riscv64-v0.18.4.4.tar.bz2",
          "dir": "monero-linux-riscv64-v0.18.4.4",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "riscv64"
        },
        {
          "hash": "eb81b71f029884ab5fec76597be583982c95fd7dc3fc5f5083a422669cee311e",
          "zip": "monero-android-armv8-v0.18.4.4.tar.bz2",
          "dir": "monero-android-armv8-v0.18.4.4",
          "ext": "tar.bz2",
          "os": "android",
          "arch": "armv8"
        },
        {
          "hash": "7c2ad18ca3a1ad5bc603630ca935a753537a38a803e98d645edd6a3b94a5f036",
          "zip": "monero-android-armv7-v0.18.4.4.tar.bz2",
          "dir": "monero-android-armv7-v0.18.4.4",
          "ext": "tar.bz2",
          "os": "android",
          "arch": "armv7"
        },
        {
          "hash": "bc539178df23d1ae8b69569d9c328b5438ae585c0aacbebe12d8e7d387a745b0",
          "zip": "monero-freebsd-x64-v0.18.4.4.tar.bz2",
          "dir": "monero-freebsd-x64-v0.18.4.4",
          "ext": "tar.bz2",
          "os": "freebsd",
          "arch": "x64"
        }
      ]
    },
    {
      "zips": [
        {
          "hash": "bd9f615657c35d2d7dd9a5168ad54f1547dbf9a335dee7f12fab115f6f394e36",
          "zip": "monero-win-x64-v0.18.4.3.zip",
          "dir": "monero-win-x64-v0.18.4.3",
          "ext": "zip",
          "os": "win",
          "arch": "x64"
        },
        {
          "hash": "e642ed7bbfa34c30b185387fa553aa9c3ea608db1f3fc0e9332afa9b522c9c1a",
          "zip": "monero-win-x86-v0.18.4.3.zip",
          "dir": "monero-win-x86-v0.18.4.3",
          "ext": "zip",
          "os": "win",
          "arch": "x86"
        },
        {
          "hash": "a8d8273b14f31569f5b7aa3063fbd322e3caec3d63f9f51e287dfc539c7f7d61",
          "zip": "monero-mac-x64-v0.18.4.3.tar.bz2",
          "dir": "monero-mac-x64-v0.18.4.3",
          "ext": "tar.bz2",
          "os": "mac",
          "arch": "x64"
        },
        {
          "hash": "bab9a6d3c2ca519386cff5ff0b5601642a495ed1a209736acaf354468cba1145",
          "zip": "monero-mac-armv8-v0.18.4.3.tar.bz2",
          "dir": "monero-mac-armv8-v0.18.4.3",
          "ext": "tar.bz2",
          "os": "mac",
          "arch": "armv8"
        },
        {
          "hash": "3a7b36ae4da831a4e9913e0a891728f4c43cd320f9b136cdb6686b1d0a33fafa",
          "zip": "monero-linux-x64-v0.18.4.3.tar.bz2",
          "dir": "monero-linux-x64-v0.18.4.3",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "x64"
        },
        {
          "hash": "e0b51ca71934c33cb83cfa8535ffffebf431a2fc9efe3acf2baad96fb6ce21ec",
          "zip": "monero-linux-x86-v0.18.4.3.tar.bz2",
          "dir": "monero-linux-x86-v0.18.4.3",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "x86"
        },
        {
          "hash": "b1cc5f135de3ba8512d56deb4b536b38c41addde922b2a53bf443aeaf2a5a800",
          "zip": "monero-linux-armv8-v0.18.4.3.tar.bz2",
          "dir": "monero-linux-armv8-v0.18.4.3",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "armv8"
        },
        {
          "hash": "3ac83049bc565fb5238501f0fa629cdd473bbe94d5fb815088af8e6ff1d761cd",
          "zip": "monero-linux-armv7-v0.18.4.3.tar.bz2",
          "dir": "monero-linux-armv7-v0.18.4.3",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "armv7"
        },
        {
          "hash": "95baaa6e8957b92caeaed7fb19b5c2659373df8dd5f4de2601ed3dae7b17ce2f",
          "zip": "monero-linux-riscv64-v0.18.4.3.tar.bz2",
          "dir": "monero-linux-riscv64-v0.18.4.3",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "riscv64"
        },
        {
          "hash": "1aebd24aaaec3d1e87a64163f2e30ab2cd45f3902a7a859413f6870944775c21",
          "zip": "monero-android-armv8-v0.18.4.3.tar.bz2",
          "dir": "monero-android-armv8-v0.18.4.3",
          "ext": "tar.bz2",
          "os": "android",
          "arch": "armv8"
        },
        {
          "hash": "4e1481835824b9233f204553d4a19645274824f3f6185d8a4b50198470752f54",
          "zip": "monero-android-armv7-v0.18.4.3.tar.bz2",
          "dir": "monero-android-armv7-v0.18.4.3",
          "ext": "tar.bz2",
          "os": "android",
          "arch": "armv7"
        },
        {
          "hash": "ff7b9c5cf2cb3d602c3dff1902ac0bc3394768cefc260b6003a9ad4bcfb7c6a4",
          "zip": "monero-freebsd-x64-v0.18.4.3.tar.bz2",
          "dir": "monero-freebsd-x64-v0.18.4.3",
          "ext": "tar.bz2",
          "os": "freebsd",
          "arch": "x64"
        }
      ]
    },

    <--- snip --->

    {
      "zips": [
        {
          "hash": "35dcc4bee4caad3442659d37837e0119e4649a77f2e3b5e80dd6d9b8fc4fb6ad",
          "zip": "monero-win-x64-v0.18.3.1.zip",
          "dir": "monero-win-x64-v0.18.3.1",
          "ext": "zip",
          "os": "win",
          "arch": "x64"
        },
        {
          "hash": "5bcbeddce32b50ebe18289d0560ebf779441526ec84d73b6a83094f092365271",
          "zip": "monero-win-x86-v0.18.3.1.zip",
          "dir": "monero-win-x86-v0.18.3.1",
          "ext": "zip",
          "os": "win",
          "arch": "x86"
        },
        {
          "hash": "7f8bd9364ef16482b418aa802a65be0e4cc660c794bb5d77b2d17bc84427883a",
          "zip": "monero-mac-x64-v0.18.3.1.tar.bz2",
          "dir": "monero-mac-x64-v0.18.3.1",
          "ext": "tar.bz2",
          "os": "mac",
          "arch": "x64"
        },
        {
          "hash": "915288b023cb5811e626e10052adc6ac5323dd283c5a25b91059b0fb86a21fb6",
          "zip": "monero-mac-armv8-v0.18.3.1.tar.bz2",
          "dir": "monero-mac-armv8-v0.18.3.1",
          "ext": "tar.bz2",
          "os": "mac",
          "arch": "armv8"
        },
        {
          "hash": "23af572fdfe3459b9ab97e2e9aa7e3c11021c955d6064b801a27d7e8c21ae09d",
          "zip": "monero-linux-x64-v0.18.3.1.tar.bz2",
          "dir": "monero-linux-x64-v0.18.3.1",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "x64"
        },
        {
          "hash": "c8553558dece79a4c23e1114fdf638b15e46899d7cf0af41457f18bbbee83986",
          "zip": "monero-linux-x86-v0.18.3.1.tar.bz2",
          "dir": "monero-linux-x86-v0.18.3.1",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "x86"
        },
        {
          "hash": "445032e88dc07e51ac5fff7034752be530d1c4117d8d605100017bcd87c7b21f",
          "zip": "monero-linux-armv8-v0.18.3.1.tar.bz2",
          "dir": "monero-linux-armv8-v0.18.3.1",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "armv8"
        },
        {
          "hash": "2ea2c8898cbab88f49423f4f6c15f2a94046cb4bbe827493dd061edc0fd5f1ca",
          "zip": "monero-linux-armv7-v0.18.3.1.tar.bz2",
          "dir": "monero-linux-armv7-v0.18.3.1",
          "ext": "tar.bz2",
          "os": "linux",
          "arch": "armv7"
        },
        {
          "hash": "6d9c7d31942dde86ce39757fd55027448ceb260b60b3c8d32ed018211eb4f1e4",
          "zip": "monero-android-armv8-v0.18.3.1.tar.bz2",
          "dir": "monero-android-armv8-v0.18.3.1",
          "ext": "tar.bz2",
          "os": "android",
          "arch": "armv8"
        },
        {
          "hash": "fc6a93eabc3fd524ff1ceedbf502b8d43c61a7805728b7ed5f9e7204e26b91f5",
          "zip": "monero-android-armv7-v0.18.3.1.tar.bz2",
          "dir": "monero-android-armv7-v0.18.3.1",
          "ext": "tar.bz2",
          "os": "android",
          "arch": "armv7"
        },
        {
          "hash": "3e2d9964a9e52c146b4d26b5eb53e691b3ba88e2468dc4fbfee4c318a367a90e",
          "zip": "monero-freebsd-x64-v0.18.3.1.tar.bz2",
          "dir": "monero-freebsd-x64-v0.18.3.1",
          "ext": "tar.bz2",
          "os": "freebsd",
          "arch": "x64"
        }
      ]
    }
  ]
}
```