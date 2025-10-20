XMR_DIR=${HOME}/dex/dcrdex/client/asset/xmr
START=`date --utc +%s`
EXIT=

echo "=== START TEST"
echo "=== Hit ^C .. ^C to quit"
echo `pwd`
echo `date`
echo "=== REMOVE OLD FILES"
rm ${XMR_DIR}/tmp_dir/*
echo "=== START TEST CLI GENERATE REFRESH"
go test -v -count=1 -run TestCliGenerateRefreshWalletCmd
EXIT=$?
echo "Exit code: $EXIT"
if [ "${EXIT}" != "0" ]
then
	exit ${EXIT}
fi

tree -L 2 ${XMR_DIR}

FIN=`date --utc +%s`
ELAPSED=$((FIN-START))
echo "Started:  ${START}s"
echo "Finished: ${FIN}s"
echo "Elapsed:  ${ELAPSED}s"
if ((ELAPSED > 30))
then
	exit 2
fi
echo "=== END TEST"
