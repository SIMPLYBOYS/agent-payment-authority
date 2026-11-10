package main

// apa-issuer ekyc (docs/commercial-ekyc.md): a consumer's attributes from an identity verification at Didit,
// not from the caller's word. It opens a verification session (or takes one already made), waits for its
// decision and writes subjects.json only for a decision that carries the document's data and was made by the
// vendor's checks. The API key is read from a 0600 file and never printed; nothing of the person (name, birth
// date, document number, images) is kept or printed.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
)

type ekycOptions struct {
	url, keyFile, consumer, buyerType, session, scenario, workflow string
	dependents                                                     []string
	wait                                                           time.Duration
}

var ekycPoll = 3 * time.Second // a test waits less

type didit struct{ url, key string }

// call: one request; an error names the status and the start of the answer, never the key.
func (c didit) call(method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.url+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "apa-issuer")
	res, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("didit %s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("didit %s %s: %w", method, path, err)
	}
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("didit %s %s: %d %.200s", method, path, res.StatusCode, strings.ReplaceAll(string(raw), c.key, "<key>"))
	}
	if json.Unmarshal(raw, out) != nil {
		return fmt.Errorf("didit %s %s: not JSON", method, path)
	}
	return nil
}

// decision is what the issuer reads of GET /v3/session/{id}/decision/ (the shape seen in the sandbox, 11/10).
type decision struct {
	SessionID   string `json:"session_id"`
	Status      string `json:"status"`
	VendorData  string `json:"vendor_data"`
	Environment string `json:"environment"`
	Trace       struct {
		DecidedBy string `json:"decided_by"`
		DecidedAt string `json:"decided_at"`
	} `json:"decision_trace"`
	IDs []struct {
		Status       string `json:"status"`
		DateOfBirth  string `json:"date_of_birth"`
		IssuingState string `json:"issuing_state"`
	} `json:"id_verifications"`
	Liveness []check `json:"liveness_checks"`
	Faces    []check `json:"face_matches"`
}

type check struct {
	Status string `json:"status"`
}

func anyApproved(cs []check) bool {
	return slices.ContainsFunc(cs, func(x check) bool { return approved(x.Status) })
}

func approved(s string) bool { return strings.EqualFold(s, "Approved") }

// derive: the country and whether the person is 18 from an approved decision, or why it is not taken. A session
// set to Approved by hand (the console's "Simulate an outcome", POST …/simulate/ with this very key) carries
// decided_by "manual" and no document: taking it would let the issuer vouch for itself.
func derive(dec decision, consumer string, today time.Time) (country, over18 string, err error) {
	switch {
	case !approved(dec.Status):
		return "", "", fmt.Errorf("the verification is %q, not Approved", dec.Status)
	case dec.VendorData != consumer:
		return "", "", fmt.Errorf("the session is %q's, not %s's", dec.VendorData, consumer)
	case dec.Trace.DecidedBy != "automatic":
		return "", "", fmt.Errorf("decided %q, not by the vendor's checks", dec.Trace.DecidedBy)
	case len(dec.IDs) == 0 || !approved(dec.IDs[0].Status):
		return "", "", errors.New("no approved document in the decision")
	case !anyApproved(dec.Liveness) || !anyApproved(dec.Faces):
		return "", "", errors.New("no approved liveness check and face match: the document's holder was not seen")
	}
	born, err := time.Parse("2006-01-02", dec.IDs[0].DateOfBirth)
	if err != nil {
		return "", "", errors.New("the document has no date of birth")
	}
	country = alpha2(dec.IDs[0].IssuingState)
	if country == "" {
		return "", "", fmt.Errorf("the document's issuing state %q is no country code", dec.IDs[0].IssuingState)
	}
	return country, fmt.Sprint(!born.AddDate(18, 0, 0).After(today)), nil
}

// alpha2: a country's ISO 3166-1 alpha-3 code (a travel document's issuing state: BRA) as its alpha-2 (BR); ""
// for anything else. The table is CLDR's regions that are countries, generated from golang.org/x/text v0.37.0
// (language.Region's ISO3); five letters a country, a space between.
func alpha2(a3 string) string {
	if len(a3) != 3 {
		return ""
	}
	for i := 0; i+5 <= len(iso3166); i += 6 {
		if iso3166[i:i+3] == a3 {
			return iso3166[i+3 : i+5]
		}
	}
	return ""
}

const iso3166 = "" +
	"ABWAW AFGAF AGOAO AIAAI ALAAX ALBAL ANDAD ANTAN AREAE ARGAR ARMAM ASCAC ASMAS ATAAQ ATFTF ATGAG ATNNQ AUSAU " +
	"AUTAT AZEAZ BDIBI BELBE BENBJ BESBQ BFABF BGDBD BGRBG BHRBH BHSBS BIHBA BLMBL BLRBY BLZBZ BMUBM BOLBO BRABR " +
	"BRBBB BRNBN BTNBT BURBU BVTBV BWABW CAFCF CANCA CCKCC CHECH CHLCL CHNCN CIVCI CMRCM CODCD COGCG COKCK COLCO " +
	"COMKM CPTCP CPVCV CRICR CTECT CUBCU CUWCW CXRCX CYMKY CYPCY CZECZ DDRDD DEUDE DGADG DHYDY DJIDJ DMADM DNKDK " +
	"DOMDO DZADZ ECUEC EGYEG ERIER ESHEH ESPES ESTEE ETHET FINFI FJIFJ FLKFK FRAFR FROFO FSMFM FXXFX GABGA GBRGB " +
	"GEOGE GGYGG GHAGH GIBGI GINGN GLPGP GMBGM GNBGW GNQGQ GRCGR GRDGD GRLGL GTMGT GUFGF GUMGU GUYGY HKGHK HMDHM " +
	"HNDHN HRVHR HTIHT HUNHU HVOHV IDNID IMNIM INDIN IOTIO IRLIE IRNIR IRQIQ ISLIS ISRIL ITAIT JAMJM JEYJE JORJO " +
	"JPNJP JTNJT KAZKZ KENKE KGZKG KHMKH KIRKI KNAKN KORKR KWTKW LAOLA LBNLB LBRLR LBYLY LCALC LIELI LKALK LSOLS " +
	"LTULT LUXLU LVALV MACMO MAFMF MARMA MCOMC MDAMD MDGMG MDVMV MEXMX MHLMH MIDMI MKDMK MLIML MLTMT MMRMM MNEME " +
	"MNGMN MNPMP MOZMZ MRTMR MSRMS MTQMQ MUSMU MWIMW MYSMY MYTYT NAMNA NCLNC NERNE NFKNF NGANG NHBNH NICNI NIUNU " +
	"NLDNL NORNO NPLNP NRUNR NTZNT NZLNZ OMNOM PAKPK PANPA PCIPC PCNPN PCZPZ PERPE PHLPH PLWPW PNGPG POLPL PRIPR " +
	"PRKKP PRTPT PRYPY PSEPS PUSPU PYFPF QATQA REURE RHORH ROURO RUSRU RWARW SAUSA SCGCS SDNSD SENSN SGPSG SGSGS " +
	"SHNSH SJMSJ SLBSB SLESL SLVSV SMRSM SOMSO SPMPM SRBRS SSDSS STPST SUNSU SURSR SVKSK SVNSI SWESE SWZSZ SXMSX " +
	"SYCSC SYRSY TAATA TCATC TCDTD TGOTG THATH TJKTJ TKLTK TKMTM TLSTL TMPTP TONTO TTOTT TUNTN TURTR TUVTV TWNTW " +
	"TZATZ UGAUG UKRUA UMIUM URYUY USAUS UZBUZ VATVA VCTVC VDRVD VENVE VGBVG VIRVI VNMVN VUTVU WAKWK WLFWF WSMWS " +
	"XKKXK YEMYE YMDYD YUGYU ZAFZA ZARZR ZMBZM ZWEZW "

func ekyc(d *credential.Dir, o ekycOptions, out io.Writer, now func() int64) error {
	if o.consumer == "" || o.keyFile == "" || slices.Contains(o.dependents, o.consumer) {
		return errors.New("ekyc needs --consumer and --key-file; a dependent is not the verified consumer")
	}
	holders, err := d.Holders()
	if err != nil {
		return err
	}
	for c := range holders { // every holder gets attributes, or the directory would refuse to issue to it
		if c != o.consumer && !slices.Contains(o.dependents, c) {
			return fmt.Errorf("ekyc: holder %s is neither --consumer nor among --dependents", c)
		}
	}
	fi, err := os.Lstat(o.keyFile)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("ekyc: the key file must be a regular file of this user only (0600)")
	}
	raw, err := os.ReadFile(o.keyFile)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("ekyc: the key file is empty")
	}
	c := didit{strings.TrimRight(o.url, "/"), string(bytes.TrimSpace(raw))}
	id := o.session
	if id == "" {
		wf := o.workflow
		if wf == "" { // the application's default KYC workflow
			var l struct {
				Results []struct {
					ID      string `json:"workflow_id"`
					Type    string `json:"workflow_type"`
					Default bool   `json:"is_default"`
				} `json:"results"`
			}
			if err := c.call("GET", "/v3/workflows/", nil, &l); err != nil {
				return err
			}
			for _, w := range l.Results {
				if w.Default && w.Type == "kyc" {
					wf = w.ID
				}
			}
			if wf == "" {
				return errors.New("ekyc: no default KYC workflow; name one with --workflow")
			}
		}
		body := map[string]string{"workflow_id": wf, "vendor_data": o.consumer, "language": "zh-TW"}
		if o.scenario != "" {
			body["sandbox_scenario"] = o.scenario
		}
		var s struct {
			ID  string `json:"session_id"`
			URL string `json:"url"`
		}
		if err := c.call("POST", "/v3/session/", body, &s); err != nil {
			return err
		}
		if s.ID == "" || s.URL == "" {
			return errors.New("ekyc: the session has no ID or URL")
		}
		id = s.ID
		fmt.Fprintf(out, "apa-issuer: %s verifies at %s (session %s); waiting up to %s\n", o.consumer, s.URL, id, o.wait)
	}
	var dec decision
	for end := time.Now().Add(o.wait); ; time.Sleep(ekycPoll) {
		dec = decision{}
		if err := c.call("GET", "/v3/session/"+id+"/decision/", nil, &dec); err != nil {
			return err
		}
		if dec.SessionID != id {
			return errors.New("ekyc: the decision is another session's")
		}
		if slices.ContainsFunc([]string{"Approved", "Declined", "Expired", "Kyc Expired", "Abandoned"}, func(s string) bool { return strings.EqualFold(s, dec.Status) }) {
			break
		}
		if !time.Now().Before(end) {
			return fmt.Errorf("ekyc: session %s is still %q after %s; nothing written", id, dec.Status, o.wait)
		}
	}
	country, over18, err := derive(dec, o.consumer, time.Unix(now(), 0).UTC())
	if err != nil {
		return fmt.Errorf("ekyc: session %s: %w; nothing written", id, err)
	}
	subjects := map[string]map[string]string{o.consumer: {"buyer_type": o.buyerType, "country": country, "age_over_18": over18}}
	for _, dep := range o.dependents { // declared by the verified consumer (a guardian's children), not verified
		subjects[dep] = map[string]string{"buyer_type": "individual", "country": country, "age_over_18": "false"}
	}
	rec := credential.EKYC{Provider: "didit", Environment: dec.Environment, SessionID: id, DecidedAt: dec.Trace.DecidedAt,
		Consumer: o.consumer, Dependents: o.dependents, Derived: map[string]string{"country": country, "age_over_18": over18}}
	if err := d.SetVerifiedSubjects(subjects, rec); err != nil {
		return err
	}
	fmt.Fprintf(out, "apa-issuer: %s verified by didit (%s), session %s: country=%s age_over_18=%s; dependents %v under 18; subjects.json and ekyc.json written\n",
		o.consumer, dec.Environment, id, country, over18, o.dependents)
	return nil
}
