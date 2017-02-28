package jobs

import (
	"fmt"

	"github.com/liam-lai/ptt-alertor/ptt/board"
	"github.com/liam-lai/ptt-alertor/user"
)

type GenBoards struct {
}

func (gb GenBoards) Run() {
	usrs := new(user.Users).All()
	bds := new(board.Boards).All()
	boardNameBool := make(map[string]bool)
	for _, bd := range bds {
		boardNameBool[bd.Name] = true
	}

	for _, usr := range usrs {
		for _, sub := range usr.Subscribes {
			if !boardNameBool[sub.Board] {
				bd := new(board.Board)
				bd.Name = sub.Board
				fmt.Println(bd.Name)
				bd.Create()
			}
		}
	}
}