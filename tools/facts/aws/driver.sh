#!/bin/bash
# driver.sh STEP [RELEASE] -- drives the AWS Intel Mac facts harvest from a
# cloud session over the EC2, SSM and S3 APIs. Each step is short enough to
# run as one background command; state lives in $STATE.
#   allocate            allocate one mac1.metal Dedicated Host
#   launch RELEASE      launch RELEASE's AMI on the host, wait for SSM
#   harvest RELEASE     run harvest.sh there through SSM Run Command, wait
#   finish              terminate the instance, wait until the host is free
#   release             release the host (only after its 24 hours)
set -euo pipefail
: "${AWS:=aws}" "${STATE:=$HOME/.dockhand-facts-aws}"
export AWS_DEFAULT_REGION=${AWS_DEFAULT_REGION:-us-west-2}
bucket=dockhand-facts-$($AWS sts get-caller-identity --query Account --output text)
mkdir -p "$STATE"
get() { cat "$STATE/$1" 2>/dev/null || true; }
put() { printf %s "$2" > "$STATE/$1"; }
log() { echo "== $(date -u +%H:%M:%S) $*"; }

ami() {
  case $1 in
    monterey|ventura|sonoma|sequoia)
      $AWS ssm get-parameter --name "/aws/service/ec2-macos/$1/x86_64_mac/latest/image_id" --query Parameter.Value --output text ;;
    bigsur) newest 'amzn-ec2-macos-11.*' ;;
    catalina) newest 'amzn-ec2-macos-10.15.*' ;;
    mojave) newest 'amzn-ec2-macos-10.14.*' ;;
  esac
}
newest() {
  $AWS ec2 describe-images --owners amazon --include-deprecated \
    --filters "Name=name,Values=$1" Name=architecture,Values=x86_64_mac \
    --query 'sort_by(Images,&CreationDate)[-1].ImageId' --output text
}

case $1 in
allocate)
  for az in us-west-2a us-west-2b us-west-2c us-west-2d; do
    if host=$($AWS ec2 allocate-hosts --instance-type mac1.metal --availability-zone "$az" --quantity 1 \
        --tag-specifications 'ResourceType=dedicated-host,Tags=[{Key=Name,Value=dockhand-facts}]' \
        --query 'HostIds[0]' --output text 2>"$STATE/allocate.err"); then
      put host "$host"; put az "$az"; put allocated "$(date -u +%s)"
      log "allocated $host in $az"; exit 0
    fi
    log "$az: $(tail -1 "$STATE/allocate.err")"
  done
  exit 1 ;;
launch)
  release=$2 host=$(get host) az=$(get az)
  image=$(ami "$release")
  subnet=$($AWS ec2 describe-subnets --filters Name=availability-zone,Values="$az" Name=default-for-az,Values=true --query 'Subnets[0].SubnetId' --output text)
  sg=$($AWS ec2 describe-security-groups --filters Name=group-name,Values=dockhand-facts --query 'SecurityGroups[0].GroupId' --output text)
  until [ "$($AWS ec2 describe-hosts --host-ids "$host" --query 'Hosts[0].State' --output text)" = available ]; do sleep 60; done
  instance=$($AWS ec2 run-instances --instance-type mac1.metal --image-id "$image" \
    --placement "HostId=$host" --subnet-id "$subnet" --security-group-ids "$sg" \
    --iam-instance-profile Name=dockhand-facts-ssm \
    --block-device-mappings 'DeviceName=/dev/sda1,Ebs={VolumeSize=200,VolumeType=gp3,DeleteOnTermination=true}' \
    --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=dockhand-facts-$release}]" \
    --query 'Instances[0].InstanceId' --output text)
  put instance "$instance"; put release "$release"
  log "launched $instance ($release, $image); waiting for SSM"
  until [ "$($AWS ssm describe-instance-information --filters "Key=InstanceIds,Values=$instance" --query 'InstanceInformationList[0].PingStatus' --output text)" = Online ]; do sleep 60; done
  log "$instance online" ;;
harvest)
  release=$2 instance=$(get instance)
  command=$($AWS ssm send-command --instance-ids "$instance" --document-name AWS-RunShellScript \
    --comment "dockhand facts $release" --output-s3-bucket-name "$bucket" --output-s3-key-prefix "logs/$release" \
    --parameters "{\"commands\":[\"export PATH=/usr/local/bin:/opt/homebrew/bin:\$PATH\",\"aws s3 cp s3://$bucket/tools/harvest.sh /tmp/harvest.sh\",\"bash /tmp/harvest.sh $bucket $release\"],\"executionTimeout\":[\"10800\"]}" \
    --query Command.CommandId --output text)
  put command "$command"
  log "command $command"
  while :; do
    status=$($AWS ssm get-command-invocation --command-id "$command" --instance-id "$instance" --query Status --output text 2>/dev/null || echo Pending)
    case $status in Pending|InProgress|Delayed) sleep 60 ;; *) break ;; esac
  done
  log "$release: $status"
  $AWS ssm get-command-invocation --command-id "$command" --instance-id "$instance" --query StandardOutputContent --output text | tail -40
  $AWS ssm get-command-invocation --command-id "$command" --instance-id "$instance" --query StandardErrorContent --output text | tail -20
  [ "$status" = Success ] ;;
finish)
  instance=$(get instance) host=$(get host)
  $AWS ec2 terminate-instances --instance-ids "$instance" >/dev/null
  log "terminated $instance; waiting for the host's wipe"
  sleep 120
  until [ "$($AWS ec2 describe-hosts --host-ids "$host" --query 'Hosts[0].State' --output text)" = available ]; do sleep 120; done
  log "host available" ;;
release)
  host=$(get host) allocated=$(get allocated)
  if [ $(( $(date -u +%s) - allocated )) -lt 86400 ]; then echo "the host's 24 hours aren't up" >&2; exit 1; fi
  $AWS ec2 release-hosts --host-ids "$host" ;;
*) echo "usage: driver.sh allocate|launch R|harvest R|finish|release" >&2; exit 2 ;;
esac
