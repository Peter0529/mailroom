package courier

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/nyaruka/mailroom/models"
	"github.com/pkg/errors"
)

const (
	highPriority    = 1
	defaultPriority = 0
)

// QueueMessages queues a message to courier
func QueueMessages(rc redis.Conn, msgs []*models.Msg) error {
	if len(msgs) == 0 {
		return nil
	}

	now := time.Now()
	epochMS := strconv.FormatFloat(float64(now.UnixNano()/int64(time.Microsecond))/float64(1000000), 'f', 6, 64)

	// TODO: figure out priority better
	priority := defaultPriority

	// we batch msgs by channel uuid
	batch := make([]*models.Msg, 0, len(msgs))
	currentChannel := msgs[0].Channel()

	for _, msg := range msgs {
		// no channel, continue
		if msg.ChannelUUID == "" {
			continue
		}

		// nil channel object but have channel UUID? that's an error
		if msg.Channel() == nil {
			return errors.Errorf("msg passed in without channel set")
		}

		// same channel? add to batch
		if msg.Channel() == currentChannel {
			batch = append(batch, msg)
		}

		// different channel? queue it up
		if msg.Channel() != currentChannel {
			batchJSON, err := json.Marshal(batch)
			if err != nil {
				return err
			}
			_, err = queueMsg.Do(rc, epochMS, "msgs", currentChannel.UUID(), currentChannel.TPS(), priority, batchJSON)
			if err != nil {
				return err
			}

			currentChannel = msg.Channel()
			batch = []*models.Msg{msg}
		}
	}

	// any remaining in our batch, queue it up
	if len(batch) > 0 {
		batchJSON, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		_, err = queueMsg.Do(rc, epochMS, "msgs", currentChannel.UUID(), currentChannel.TPS(), priority, batchJSON)
		if err != nil {
			return err
		}
	}

	return nil
}

var queueMsg = redis.NewScript(6, `
-- KEYS: [EpochMS, QueueType, QueueName, TPS, Priority, Value]

-- first push onto our specific queue
-- our queue name is built from the type, name and tps, usually something like: "msgs:uuid1-uuid2-uuid3-uuid4|tps"
local queueKey = KEYS[2] .. ":" .. KEYS[3] .. "|" .. KEYS[4]

-- our priority queue name also includes the priority of the message (we have one queue for default and one for bulk)
local priorityQueueKey = queueKey .. "/" .. KEYS[5]
redis.call("zadd", priorityQueueKey, KEYS[1], KEYS[6])
local tps = tonumber(KEYS[4])

-- if we have a TPS, check whether we are currently throttled
local curr = -1
if tps > 0 then
  local tpsKey = queueKey .. ":tps:" .. math.floor(KEYS[1])
  curr = tonumber(redis.call("get", tpsKey))
end

-- if we aren't then add to our active
if not curr or curr < tps then
redis.call("zincrby", KEYS[2] .. ":active", 0, queueKey)
  return 1
else
  return 0
end
`)
