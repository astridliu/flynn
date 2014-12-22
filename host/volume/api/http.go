package volumeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/zfs"
	"github.com/flynn/flynn/pkg/httphelper"
)

func CreateProvider(vman *volume.Manager, w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	rh := httphelper.NewReponseHelper(w)

	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		rh.Error(err)
		return
	}

	var provider volume.Provider
	pspec := &volume.ProviderSpec{}
	if err = json.Unmarshal(data, &pspec); err != nil {
		rh.Error(err)
		return
	}
	switch pspec.Kind {
	case "zfs":
		if parentDataset, ok := pspec.Metadata["parent_dataset"]; ok {
			var err error // shadowing prevention for `provider` -.-
			if provider, err = zfs.NewProvider(parentDataset); err != nil {
				rh.JSON(500, err)
				return
			}
		} else {
			rh.JSON(400, errors.New("host: zfs volume provider requires a 'parent_dataset' parameter"))
			return
		}
	case "":
		rh.JSON(400, errors.New("host: volume provider kind must not be blank"))
		return
	default:
		rh.JSON(400, fmt.Errorf("host: volume provider kind '%s' is not known"))
		return
	}

	// REVIEW/DESIGN: do we let clients pick the IDs here, or do we pick and return it?
	if err := vman.AddProvider("todo-ID", provider); err != nil {
		switch err {
		case volume.ProviderAlreadyExists:
			rh.JSON(400, err)
			return
		default:
			rh.JSON(500, err)
			return
		}
	}

	ret := struct{}{}
	rh.JSON(200, ret)
}

func Create(vman *volume.Manager, w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	rh := httphelper.NewReponseHelper(w)
	providerID := ps.ByName("provider_id")

	vol, err := vman.NewVolumeFromProvider(providerID)
	if err == volume.NoSuchProvider {
		// REVIEW/DESIGN: what's our API standard for this?  404 might be less helpful than 400'ing because it's ambiguous with a broader kind of you-dun-goofed or say for example a wild api verson mismatch
		rh.JSON(404, err)
		return
	}

	rh.JSON(200, vol.ID()) // TODO: perhaps 'inspect' return structure, after we pin that down?
}

func Snapshot(vman *volume.Manager, w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	// TODO
}
