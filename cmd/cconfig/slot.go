package main

import (
	"codis/pkg/models"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/docopt/docopt-go"
	log "github.com/ngaut/logging"
	"github.com/nu7hatch/gouuid"
)

func cmdSlot(argv []string) (err error) {
	usage := `usage:
	cconfig slot init
	cconfig slot info <slot_id>
	cconfig slot set <slot_id> <group_id> <status>
	cconfig slot range-set <slot_from> <slot_to> <group_id> <status>
	cconfig slot migrate <slot_from> <slot_to> <group_id> [--delay=<delay_time_in_ms>]

options:
    TODO
`

	args, err := docopt.Parse(usage, argv, true, "", false)
	if err != nil {
		log.Error(err)
		return err
	}
	log.Debug(args)

	// no need to lock here
	// locked in runmigratetask
	if args["migrate"].(bool) {
		delay := 0
		groupId, err := strconv.Atoi(args["<group_id>"].(string))
		if args["--delay"] != nil {
			delay, err = strconv.Atoi(args["--delay"].(string))
			if err != nil {
				log.Warning(err)
				return err
			}
		}
		slotFrom, err := strconv.Atoi(args["<slot_from>"].(string))
		if err != nil {
			log.Warning(err)
			return err
		}

		slotTo, err := strconv.Atoi(args["<slot_to>"].(string))
		if err != nil {
			log.Warning(err)
			return err
		}
		return runSlotMigrate(slotFrom, slotTo, groupId, delay)
	}

	zkLock.Lock(fmt.Sprintf("slot, %+v", argv))
	defer func() {
		err := zkLock.Unlock()
		if err != nil {
			log.Error(err)
		}
	}()

	if args["init"].(bool) {
		return runSlotInit()
	}

	if args["info"].(bool) {
		slotId, err := strconv.Atoi(args["<slot_id>"].(string))
		if err != nil {
			log.Warning(err)
			return err
		}
		return runSlotInfo(slotId)
	}

	groupId, err := strconv.Atoi(args["<group_id>"].(string))
	if err != nil {
		log.Warning(err)
		return err
	}

	if args["set"].(bool) {
		slotId, err := strconv.Atoi(args["<slot_id>"].(string))
		status := args["<status>"].(string)
		if err != nil {
			log.Warning(err)
			return err
		}
		return runSlotSet(slotId, groupId, status)
	}

	if args["range-set"].(bool) {
		status := args["<status>"].(string)
		slotFrom, err := strconv.Atoi(args["<slot_from>"].(string))
		if err != nil {
			log.Warning(err)
			return err
		}
		slotTo, err := strconv.Atoi(args["<slot_to>"].(string))
		if err != nil {
			log.Warning(err)
			return err
		}
		return runSlotRangeSet(slotFrom, slotTo, groupId, status)
	}
	return nil
}

func runSlotInit() error {
	err := models.InitSlotSet(zkConn, productName, models.DEFAULT_SLOT_NUM)
	if err != nil {
		return err
	}
	return nil
}

func runSlotInfo(slotId int) error {
	s, err := models.GetSlot(zkConn, productName, slotId)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, " ", "  ")
	fmt.Println(string(b))
	return nil
}

func runSlotRangeSet(fromSlotId, toSlotId int, groupId int, status string) error {
	err := models.SetSlotRange(zkConn, productName, fromSlotId, toSlotId, groupId, models.SlotStatus(status))
	if err != nil {
		return err
	}
	return nil
}

func runSlotSet(slotId int, groupId int, status string) error {
	slot := models.NewSlot(productName, slotId)
	slot.GroupId = groupId
	slot.State.Status = models.SlotStatus(status)
	ts := time.Now().Unix()
	slot.State.LastOpTs = strconv.FormatInt(ts, 10)
	if err := slot.Update(zkConn); err != nil {
		return err
	}
	return nil
}

func runSlotMigrate(fromSlotId, toSlotId int, newGroupId int, delay int) error {
	t := &MigrateTask{}
	t.Delay = delay
	t.FromSlot = fromSlotId
	t.ToSlot = toSlotId
	t.NewGroupId = newGroupId
	t.Status = "migrating"
	t.CreateAt = strconv.FormatInt(time.Now().Unix(), 10)
	u, err := uuid.NewV4()
	if err != nil {
		return err
	}
	t.Id = u.String()
	t.stopChan = make(chan struct{})

	// run migrate
	if ok, err := preMigrateCheck(t); ok {
		err = RunMigrateTask(t)
		if err != nil {
			log.Warning(err)
			return err
		}
	} else {
		log.Warning(err)
		return err
	}

	return nil
}