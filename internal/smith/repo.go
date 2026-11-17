package smith

import (
	"errors"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
)

func missingHead(err error) error {
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return errors.New("repository has no commits")
	}
	return err
}

type identity struct {
	name  string
	email string
}

func loadIdentity(repo *git.Repository) (identity, error) {
	cfg, err := repo.Config()
	if err != nil {
		return identity{}, err
	}
	if cfg == nil {
		return identity{}, errors.New("git config is missing")
	}
	if cfg.User.Name == "" {
		return identity{}, errors.New("git user.name must be set")
	}
	if cfg.User.Email == "" {
		return identity{}, errors.New("git user.email must be set")
	}
	return identity{name: cfg.User.Name, email: cfg.User.Email}, nil
}
