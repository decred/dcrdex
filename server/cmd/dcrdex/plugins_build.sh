#!/bin/bash
# This file will build dcrdex; 
# build all the plugins in asset/build folder and move those to $HOME/.dcrdex/assets

dir=$(pwd)
# build dcrdex
echo "Building dcrdex..."
go build $dir
echo "Done!"
cd ../../asset/build
homeAsset=${HOME}/.dcrdex/assets
mkdir -p $homeAsset
echo "Start building assets"
for FILE in `ls -l ./`
  do
    if test -d $FILE
    then
      echo "Building $FILE asset"
      if go build -buildmode=plugin ./$FILE ; then
        echo "Done!"
        echo "Moving $FILE.so to assets folder."
        mv $FILE.so $homeAsset/$FILE.so
        echo "Done!"
      fi
      
    fi
done
