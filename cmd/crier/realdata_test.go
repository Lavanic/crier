package main

// green/red regression test from real data: every alert crier sent to
// my phone 2026-07-08 to 2026-07-17, hand-labeled across two audits.
// green rows (keep=true) must keep alerting no matter how the filters
// get tuned, red rows must stay dead. runs against the committed
// config.yaml so tuning immediately shows which real alerts flip.
// second audit added the wrong-year rule: I graduate dec 2026, so
// 2026-and-earlier cohort tags flipped a few first-audit greens red

import (
	"testing"

	"github.com/Lavanic/crier/internal/config"
	"github.com/Lavanic/crier/internal/filter"
	"github.com/Lavanic/crier/internal/sources"
)

type realAlert struct {
	company  string
	title    string
	location string
	url      string
	category string
	killedBy string // why the precision pass drops it, "" = must alert
	keep     bool
}

var realAlerts = []realAlert{
	// #1
	{"Vanguard", "Entry Level Application Engineer", "Malvern, PA", "https://vanguard.wd5.myworkdayjobs.com/en-US/vanguard_external/job/Malvern-PA/Entry-Level-Application-Engineer---2026-Start-Date_168908", "Software", "2026 start in url", false},
	// #2
	{"Vanguard", "Entry Level Application Engineer", "Charlotte, NC", "https://vanguard.wd5.myworkdayjobs.com/en-US/vanguard_external/job/Charlotte-NC/Entry-Level-Application-Engineer----2026-Start-Date_171145", "Software", "2026 start in url", false},
	// #3
	{"Fujifilm", "Production Engineer 1", "Pueblo, CO", "https://uscareers-fujifilm.icims.com/jobs/38036/job?mobile=true&needsRedirect=false", "AI/ML/Data", "exclude:production engineer", false},
	// #4
	{"Trend Micro", "Applied AI Junior Web Developer", "Ottawa, ON, Canada", "https://trendmicro.wd3.myworkdayjobs.com/External/job/Ottawa/Applied-AI-Junior-Web-Developer---Ottawa--ON_R0009513", "Software", "location", false},
	// #5
	{"WVUMedicine", "Systems Analyst Associate - Software Trainer - Inpatient and Outpatient Provider Training", "Morgantown, WV", "https://wvumedicine.wd1.myworkdayjobs.com/WVUH/job/WVUH-Windwood/Associate-Systems-Analyst---Software-Trainer--Inpatient-and-Outpatient-Provider-Training----Hybrid_JR26-34833", "Software", "exclude:trainer", false},
	// #6
	{"GE Healthcare", "Field Service Engineer 1", "Cincinnati, OH", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/Field-Service-Engineer-I---Cincinnati--OH_R4042421-1", "Hardware", "exclude:field service", false},
	// #7
	{"GE Healthcare", "Field Service Engineer 1", "St. Louis, MO", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/Field-Service-Engineer-1---Missouri_R4040983-1", "Hardware", "exclude:field service", false},
	// #8
	{"GE Healthcare", "Field Engineer 1", "El Paso, TX", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/El-Paso/Field-Engineer-1_R4042436-1", "Hardware", "exclude:field engineer", false},
	// #9
	{"Pfizer", "Manufacturing Execution Systems Engineer 1 - Manufacturing Execution Systems", "Groton, CT", "https://pfizer.wd1.myworkdayjobs.com/en-US/PfizerCareers/job/United-States---Connecticut---Groton/DPMS-Manufacturing-Execution-Systems--MES--Engineer_4959813-1", "AI/ML/Data", "exclude:manufacturing", false},
	// #10
	{"Trend Micro", "Applied AI Junior Web Developer", "Ottawa, ON, Canada", "https://trendmicro.wd3.myworkdayjobs.com/External/job/Ottawa/Applied-AI-Junior-Web-Developer---Ottawa--ON_R0009512", "Software", "location", false},
	// #11
	{"Lendable", "Graduate Analytics Engineer", "London, UK", "https://jobs.ashbyhq.com/lendable/36d47627-9b4e-4864-9f05-2dbbd1052380/application", "AI/ML/Data", "location", false},
	// #12
	{"Motorola", "Junior Software Engineer - AI Agent Platform", "Alberta, Canada | Remote in Canada", "https://motorolasolutions.wd5.myworkdayjobs.com/Careers/job/Ontario-Remote-Work/Junior-Software-Engineer--AI-Agent-Platform_R66146", "Software", "location", false},
	// #13
	{"RoadRunner Recycling", "Software Engineer 1 - Full-Stack", "Remote in USA", "https://job-boards.greenhouse.io/roadrunner/jobs/4307451009", "Software", "", true},
	// #14
	{"astranis", "Network Planning Sales Engineer Associate (Fall 2026)", "San Francisco", "https://job-boards.greenhouse.io/astranis/jobs/4693083006", "", "exclude:sales engineer", false},
	// #15
	{"andurilindustries", "Software Corporate Technology Rotation Program", "Costa Mesa, California, United States", "https://boards.greenhouse.io/andurilindustries/jobs/5166390007?gh_jid=5166390007", "", "", true},
	// #16
	{"DMC Engineering", "Entry Level Embedded Engineer", "Denver, CO", "https://www.dmcinfo.com/careers/open-positions?gh_jid=5136284008", "Hardware", "", true},
	// #17
	{"RTX", "Raytheon Full Time - Software Effectors Engineer I", "Tucson, AZ", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-AZ-TUCSON-805--1151-E-Hermans-Rd--BLDG-805/XMLNAME-2026-Raytheon-Full-Time---Software-Effectors-Engineer-I---Tucson--AZ--Onsite-_01835713", "Hardware", "2026 cohort in url", false},
	// #18
	{"Travelers", "Software Engineer 1 - AI Driven", "Hartford, CT | St Paul, MN", "https://travelers.wd5.myworkdayjobs.com/External/job/CT---Hartford/Software-Engineer-I--AI-Driven-_R-51215", "Software", "", true},
	// #19
	{"University of British Columbia", "Junior Data Developer", "Vancouver, BC, Canada", "https://ubc.wd10.myworkdayjobs.com/ubcstaffjobs/job/UBC-Vancouver-Campus---Vancouver-BC-Canada/Junior-Data-Developer_JR25210", "AI/ML/Data", "location", false},
	// #20
	{"Cincinnati Children’s Hospital and Medical Center", "Application Developer 1 - Power Platform", "Cincinnati, OH", "https://cincinnatichildrens.wd5.myworkdayjobs.com/careersatcincinnatichildrens/job/Offices-at-Vernon-Place/Application-Developer-I--Power-Platform_JR222607", "Software", "exclude:power platform", false},
	// #21
	{"CACI", "Computer Hardware Engineer 1", "Aberdeen, MD | Aiea, HI", "https://caci.wd1.myworkdayjobs.com/external/job/Camp-Smith-HI-US/Computer-Hardware-Engineer-I_328772", "Hardware", "category Hardware", false},
	// #22
	{"Rocket Companies", "IOS Mobile Software Developer 1", "Seattle, WA | SF | Detroit, MI", "https://quickenloans.wd5.myworkdayjobs.com/rocket_careers/job/Seattle-WA/iOS-Mobile-Software-Developer-I_R-083670-1", "Software", "", true},
	// #23
	{"andurilindustries", " Early Career Product Operations Rotation Program ", "Costa Mesa, California, United States", "https://boards.greenhouse.io/andurilindustries/jobs/5181983007?gh_jid=5181983007", "", "exclude:product operations", false},
	// #24
	{"The Boeing Company", "Development Security & Operations Associate - Devsecops - Software Engineer", "Colorado Springs, CO", "https://boeing.wd1.myworkdayjobs.com/external_subsidiary/job/USA---Colorado-Springs-CO/Development-Security---Operations--DevSecOps--Software-Engineer--Associate-or-Mid-Level-_JR2026515911", "Software", "", true},
	// #25
	{"RTX", "FPGA Engineer 1", "McKinney, TX", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-TX-MCKINNEY-513WD--2501-W-University-Dr--WING-D-BLDG/FPGA-Engineer-I_01857935", "Hardware", "category Hardware", false},
	// #26
	{"Red Hat", "Associate Software Engineer", "Durham, NC", "https://redhat.wd5.myworkdayjobs.com/jobs/job/Durham/Associate-Software-Engineer_R-058395", "Software", "", true},
	// #27
	{"Anduril", "Software Corporate Technology Rotation Program", "Newport Beach, CA", "https://boards.greenhouse.io/andurilindustries/jobs/5166390007", "Software", "cross-post of #15", false},
	// #28
	{"Northrop Grumman", "Software Engineer 1 or 2", "Aurora, CO | Morrisville, NC | Fairfax, VA", "https://ngc.wd1.myworkdayjobs.com/Northrop_Grumman_External_Site/job/United-States-Colorado-Aurora/Software-Engineer-Level-1-or-2_R10239507", "Software", "", true},
	// #29
	{"Northrop Grumman", "Associate Cyber Software Engineer", "Chantilly, VA", "https://ngc.wd1.myworkdayjobs.com/Northrop_Grumman_External_Site/job/United-States-Virginia-Chantilly/R10206390-22026-Associate-Cyber-Software-Engineer---Chantilly-VA_R10236425", "Software", "", true},
	// #30
	{"Micron Technology", "New College Grad - Semiconductor Verification Design Engineer - DRAM Products Group", "Boise, ID", "https://micron.wd1.myworkdayjobs.com/External/job/Boise-ID---Main-Site/New-College-Grad---Semiconductor-Verification-Design-Engineer--DRAM-Products-Group_JR104793", "Hardware", "category Hardware", false},
	// #31
	{"The Boeing Company", "Development Security & Operations Associate - Devsecops - Software Engineer", "Colorado Springs, CO", "https://boeing.wd1.myworkdayjobs.com/EXTERNAL_CAREERS/job/USA---Colorado-Springs-CO/Development-Security---Operations--DevSecOps--Software-Engineer--Associate-or-Mid-Level-_JR2026515911-1", "Software", "cross-post of #24", false},
	// #32
	{"The Walt Disney Company", "Product Software Engineer 1", "Santa Monica, CA", "https://disney.wd5.myworkdayjobs.com/disneycareer/job/Santa-Monica-CA-USA/Product-Software-Engineer-I_10151711", "Software", "", true},
	// #33
	{"Built Technologies", "Software Engineer 1 - Internal Tooling", "Nashville, TN", "https://job-boards.greenhouse.io/getbuilt/jobs/4713164005", "Software", "", true},
	// #34
	{"RTX", "Software Engineer 1", "Richardson, TX", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-TX-RICHARDSON-C17--1717-Cityline-Dr--CITYLINE-C17/Software-Engineer-I--Onsite-_01857780", "Software", "", true},
	// #35
	{"RTX", "Embedded Linux Software Engineer 1", "Cedar Rapids, IA", "https://globalhr.wd5.myworkdayjobs.com/fr-CA/Private_Posting_No_TMP/job/US-IA-CEDAR-RAPIDS-121--350-Collins-Rd-NE--BLDG-121/Embedded-Linux-Software-Engineer-I--Onsite-_01793223-2", "Hardware", "", true},
	// #36
	{"EvenUp", "Software Engineer – New Grad - Cases Product", "Toronto, ON, Canada | SF", "https://jobs.ashbyhq.com/evenup/41488eae-50a9-4ad3-b6e0-2fd28efb238e/application", "Software", "", true},
	// #37
	{"Thoughtworks", "Graduate Developer", "Chicago, IL", "https://www.thoughtworks.com/careers/jobs/8037730?gh_jid=8037730", "Software", "", true},
	// #38
	{"Quora", "Machine Learning Engineer New Grad", "Remote in USA | Remote in Canada", "https://jobs.ashbyhq.com/quora/3eb7e80e-6a0d-41b6-8ee4-f62421c486e4/application", "AI/ML/Data", "", true},
	// #39
	{"twilio", "Technical Support Engineer 1", "Remote - India", "https://job-boards.greenhouse.io/twilio/jobs/8016229", "", "exclude:technical support", false},
	// #40
	{"Abbott", "Field Service Engineer 1", "Detroit, MI", "https://abbott.wd5.myworkdayjobs.com/abbottcareers/job/United-States---Michigan---Detroit/Field-Service-Engineer-I--Greater-Detroit--MI_31155642-2", "Hardware", "exclude:field service", false},
	// #41
	{"RTX", "Digital Product Configuration Management Engineer 1", "Burlington, MA", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-MA-WOBURN-WB1--235-Presidential-Way--SPENCER-BLDG/XMLNAME-2026-Raytheon-Full-Time-Digital-Product-Configuration-Management-Engineer-I--Onsite-_01855446", "AI/ML/Data", "exclude:configuration management", false},
	// #42
	{"GE Healthcare", "Field Service Engineer 1", "Kansas City, MO | Kansas City, KS", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/Field-Service-Engineer-I_R4042607", "Hardware", "exclude:field service", false},
	// #43
	{"CACI", "Junior Software Engineer", "Springfield, VA | St. Louis, MO | Denver, CO | Dulles, VA", "https://caci.wd1.myworkdayjobs.com/external/job/Denver-CO-US/Junior-Software-Engineer_327867", "Software", "", true},
	// #44
	{"samsara", "Sr. Software Engineer I, Cloud Security Platform ", "Remote - UK", "https://www.samsara.com/company/careers/roles/8042387?gh_jid=8042387", "", "exclude:sr", false},
	// #45
	{"Elwood Technologies", "Graduate Software Engineer - User Interface", "London, UK", "https://job-boards.greenhouse.io/elwoodtechnologies/jobs/6112453004", "Software", "location", false},
	// #46
	{"Regeneron Pharmaceuticals", "Process Development Engineer 1 - Purification Data Analysis", "Tarrytown, NY", "https://regeneron.wd1.myworkdayjobs.com/en-US/Careers/job/TARRYTOWN/Process-Development-Engineer-I---Purification-Data-Analysis_R48653", "AI/ML/Data", "exclude:process development", false},
	// #47
	{"Elwood Technologies", "Graduate/Junior Software Engineer - Backend", "London, UK", "https://job-boards.greenhouse.io/elwoodtechnologies/jobs/6112395004", "Software", "location", false},
	// #48
	{"rocketlab", "Propulsion Engineer I - Engine Systems", "Long Beach, CA", "https://job-boards.greenhouse.io/rocketlab/jobs/7005148003", "", "exclude:propulsion", false},
	// #49
	{"rocketlab", "Propulsion Engineer I, Engine Systems", "Stennis Space Center, MS", "https://job-boards.greenhouse.io/rocketlab/jobs/7679086003", "", "exclude:propulsion", false},
	// #50
	{"andurilindustries", "Early Career Firmware Engineer ", "Costa Mesa, California, United States", "https://boards.greenhouse.io/andurilindustries/jobs/5167865007?gh_jid=5167865007", "", "", true},
	// #51
	{"rocketlab", "Manufacturing Engineer I - Machining", "Long Beach, CA", "https://job-boards.greenhouse.io/rocketlab/jobs/6675154003", "", "exclude:manufacturing", false},
	// #52
	{"spacex", "Dec 2026 New Graduate Engineer, Mechanical (Starship)", "Starbase, TX", "https://boards.greenhouse.io/spacex/jobs/8627507002?gh_jid=8627507002", "", "exclude:mechanical", false},
	// #53
	{"spacex", "Dec 2026 New Graduate Engineer, Propulsion (Starship)", "Starbase, TX", "https://boards.greenhouse.io/spacex/jobs/8627510002?gh_jid=8627510002", "", "exclude:propulsion", false},
	// #54
	{"Medtronic", "Cloud Software Engineer 1 - Neuro", "Brooklyn Park, MN", "https://medtronic.wd1.myworkdayjobs.com/redeploymentmedtroniccareers/job/Fridley-Minnesota-United-States-of-America/Cloud-Software-Engineer-I---Neuro---Rice-Creek-Fridley--MN_R66348", "Software", "", true},
	// #55
	{"RTX", "Raytheon Software Engineer 1 - Electro-Optical/Infrared Advanced Products and Solutions", "McKinney, TX", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-TX-MCKINNEY-513WC--2501-W-University-Dr--WING-C-BLDG/XMLNAME-2026-Raytheon-Full-Time-Software-Engineer-I---EOIR-Advanced-Products-and-Solutions--Onsite-_01851718", "Software", "2026 cohort in url", false},
	// #56
	{"Pylon", "Software Engineer New Grad", "SF", "https://jobs.ashbyhq.com/pylon-labs/38814ce7-217b-40f2-9ba5-8a7733a5691d/application", "Software", "", true},
	// #57
	{"RTX", "Software Engineer 1", "San Diego, CA", "https://globalhr.wd5.myworkdayjobs.com/fr-CA/Private_Posting_No_TMP/job/US-CA-SAN-DIEGO-SD1--8650-Balboa-Ave--SAN-ANTONIO-BLDG/Software-Engineer-I--Onsite-_01858576", "Software", "", true},
	// #58
	{"rocketlab", "Spacecraft GNC Engineer I/II", "Littleton, CO", "https://job-boards.greenhouse.io/rocketlab/jobs/7660829003", "", "exclude:gnc", false},
	// #59
	{"Anduril", "Early Career Firmware Engineer", "Newport Beach, CA", "https://boards.greenhouse.io/andurilindustries/jobs/5167865007", "Hardware", "cross-post of #50", false},
	// #60
	{"NVIDIA", "ASIC Design Engineer New Grad", "Santa Clara, CA", "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite/job/US-CA-Santa-Clara/ASIC-Design-Engineer---New-College-Grad-2026_JR2020309", "Hardware", "exclude:asic", false},
	// #61
	{"Micron Technology", "Design Engineer New Grad - Design Engineer - DRAM Technology and Products", "Boise, ID", "https://micron.wd1.myworkdayjobs.com/External/job/Boise-ID---Main-Site/New-College-Grad---Design-Engineer--DRAM-Technology-and-Products_JR105519", "Hardware", "category Hardware", false},
	// #62
	{"Leidos", "Entry Level Software Developer", "St. Louis, MO", "https://leidos.wd5.myworkdayjobs.com/External/job/St-Louis-MO/Entry-Level-Software-Developer_R-00186923", "Software", "", true},
	// #63
	{"Cadence Design Systems", "Software Engineer New Grad - Undergrads", "Burlington, MA", "https://cadence.wd1.myworkdayjobs.com/University_Talent/job/Burlington-MA/Software-Engineer--New-College-Grad-2026--Undergrads-_R54894", "Software", "2026 cohort in url", false},
	// #64
	{"Cadence Design Systems", "Software Engineer New Grad - Undergrads", "Burlington, MA", "https://cadence.wd1.myworkdayjobs.com/University_Talent_NCG/job/Burlington-MA/Software-Engineer--New-College-Grad-2026--Undergrads-_R54894-3", "Software", "2026 cohort in url", false},
	// #65
	{"RTX", "Software Engineer 1", "Fulton, MD", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-MD-FULTON-8170--8170-Maple-Lawn-Blvd--MAPLE-LAWN-Ste-190-200--300/Software-Engineer-I--Onsite-_01858115", "Software", "", true},
	// #66
	{"Cadence Design Systems", "Software Engineer New Grad - Undergrads", "Burlington, MA", "https://cadence.wd1.myworkdayjobs.com/Univ_Careers/job/Burlington-MA/Software-Engineer--New-College-Grad-2026--Undergrads-_R54894-1", "Software", "2026 cohort in url", false},
	// #67
	{"SimpliSafe", "Software Engineer I", "Boston, MA", "https://job-boards.greenhouse.io/simplisafe/jobs/8049515", "Software", "", true},
	// #68
	{"Uber Technologies, Inc.", "Software Engineer I", "Seattle, Washington", "https://jobs.uber.com/en/jobs/160017/?_csid=BOBcQVO6jwRuNsBKbjoAZA&effect=&sm_flow_id=92Yhskrz&state=xlA80FxvL-2272sFxrCsxXWLk7KQR60tJLE43FsTxHI%3D", "", "", true},
	// #69
	{"RTX", "Software Engineer I", "Fullerton, CA", "https://globalhr.wd5.myworkdayjobs.com/en-GB/REC_RTX_Ext_Gateway/job/US-CA-FULLERTON-676--1801-Hughes-Dr--BLDG-676/Software-Engineer-I---Onsite-_01857157", "", "", true},
	// #70
	{"SimpliSafe", "Software Engineer I", "Boston, MA", "https://job-boards.greenhouse.io/simplisafe/jobs/8049510", "Software", "", true},
	// #71
	{"U.S. Bank", "Software Engineer 1 (Backend UI and AI)", "Earth City, MO", "https://usbank.wd1.myworkdayjobs.com/US_Bank_Careers/job/Earth-City-MO/Software-Engineer-1--Backend-UI-and-AI-_2026-0018795", "", "", true},
	// #72
	{"L3Harris Technologies", "Associate Software Engineer", "Richardson, TX", "https://jobs.l3harris.com/job/Richardson-Associate-Software-Engineer-TX-75080/1407459900/?ats=successfactors", "Hardware", "", true},
	// #73
	{"RTX", "Embedded Cybersecurity Software Engineer 1", "Cedar Rapids, IA", "https://globalhr.wd5.myworkdayjobs.com/fr-CA/Private_Posting_No_TMP/job/US-IA-CEDAR-RAPIDS-137--855-35Th-St-NE--BLDG-137/Embedded-Cybersecurity-Software-Engineer-I--Onsite-_01829680-1", "Hardware", "", true},
	// #74
	{"The Walt Disney Company", "Product Software Engineer 1", "Bristol, CT", "https://disney.wd5.myworkdayjobs.com/disneycareer/job/Bristol-CT-USA/Product-Software-Engineer-I_10155293", "Software", "", true},
	// #75
	{"Royal Bank of Canada", "Quant Rotation Program Associate New Grad", "Toronto, ON, Canada", "https://rbc.wd3.myworkdayjobs.com/ExternalPrivatePostingStudents/job/TORONTO-Ontario-Canada/Quant-New-Grad-Rotation-Program-Associate_R-0000172286", "Quant", "location", false},
	// #76
	{"GE Healthcare", "Field Engineer 1", "Remote in USA", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/FE-1-Bloomington--Illinois_R4041927-1", "Hardware", "exclude:field engineer", false},
	// #77
	{"GE Healthcare", "Field Service Engineer 1", "Chicago, IL", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/Field-Service-Engineer-1_R4041924-1", "Hardware", "exclude:field service", false},
	// #78
	{"Becton Dickinson", "Product Development Engineer 1", "Warwick, RI", "https://bdx.wd1.myworkdayjobs.com/EXTERNAL_CAREER_SITE_USA/job/USA-RI---Warwick/Product-Development-Engineer-I_R-542366-1", "Product", "category Product", false},
	// #79
	{"Citadel Securities", "Quantitative Research Analyst – University Graduate", "London, UK | Dublin, Ireland", "https://www.citadelsecurities.com/careers/details/quantitative-research-analyst-university-graduate-europe/", "Quant", "location", false},
	// #80
	{"GE Healthcare", "Field Service Engineer 1 - Anesthesia", "Fayetteville, NC | Goldsboro, NC | Greenville, NC", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/PCS-Field-Service-Engineer-1---Anesthesia_R4043436-1", "Hardware", "exclude:field service", false},
	// #81
	{"NVIDIA", "Formal Verification Engineer – New College Grad 2026", "Santa Clara, CA | Austin, TX", "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite/job/US-TX-Austin/Formal-Verification-Engineer---New-College-Grad-2026_JR2013065", "Hardware", "category Hardware", false},
	// #82
	{"Salesforce", "AI Builder New Grad - Emerging Talent", "London, UK", "https://salesforce.wd12.myworkdayjobs.com/External_Career_Site/job/United-Kingdom---London/AI-Builder--Emerging-Talent---UK---Ireland-Market_JR342481-1", "Software", "location", false},
	// #83
	{"Kustomer", "Software Engineer – Early Career - Full Stack", "NYC", "https://jobs.ashbyhq.com/kustomer/4037272a-7fd3-4040-906b-47fde875a817/application", "Software", "", true},
	// #84
	{"Micron Technology", "Design Engineer New Grad - Design Engineer - Non-Volatile Engineering Group", "San Jose, CA", "https://micron.wd1.myworkdayjobs.com/External/job/San-Jose-CA/Principal-Design-Engineer--NVEG_JR88223", "Hardware", "category Hardware", false},
	// #85
	{"General Dynamics Mission Systems", "Infrastructure Hardware Systems Engineer Entry Level", "Chula Vista, CA", "https://careers-gdms.icims.com/jobs/73388/job?mobile=true&needsRedirect=false", "Hardware", "category Hardware", false},
	// #86
	{"HNTB", "Software Engineer 1", "Tallahassee, FL", "https://hntb.wd5.myworkdayjobs.com/hntb_careers/job/Tallahassee-FL/Software-Engineer-I_R-30741", "Software", "", true},
	// #87
	{"Ultra", "Software Engineer Associate", "Austin, TX | Remote in USA | Remote in UK | Huntsville, AL", "https://ultra.wd3.myworkdayjobs.com/ultra-careers/job/Huntsville-USA/Associate-Software-Engineer_REQ-12218-1", "Software", "", true},
	// #88
	{"Exact Sciences", "Instrumentation/Automation Service Engineer 1", "Madison, WI", "https://exactsciences.wd1.myworkdayjobs.com/en-CA/Exact_Sciences/job/US---WI---Madison/Instrumentation-Automation-Service-Engineer-I--Tues--Fri--6-00-am--4-30-pm_R26-13041-1", "Hardware", "category Hardware", false},
	// #89
	{"Dell Technologies", "Systems Development Engineer 1", "Westborough, MA", "https://iawmqy.fa.ocs.oraclecloud.com/hcmUI/CandidateExperience/en/sites/careers/job/294268", "Hardware", "category Hardware", false},
	// #90
	{"Sandisk", "SSD Firmware Engineer New Grad - SSD Firmware Engineer", "Milpitas, CA", "https://jobs.smartrecruiters.com/Sandisk/744000137191590", "Hardware", "", true},
	// #91
	{"Mechanize", "Junior Software Engineer", "SF", "https://www.mechanize.work/apply/software-engineer/?role=junior", "Software", "", true},
	// #92
	{"American Express", "Software Engineer 1 - Oracle Cloud HCM - CET Services", "Phoenix, AZ", "https://egug.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_1/job/26007827", "Software", "", true},
	// #93
	{"Parsons", "Junior Software Developer", "Remote in USA", "https://parsons.wd5.myworkdayjobs.com/en-US/search/job/US---Remote-Any-Location/Junior-Software-Developer_R183217", "Software", "", true},
	// #94
	{"NVIDIA", "Systems Software Engineer New Grad - Autonomous Systems Mapping", "Santa Clara, CA", "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite/job/US-CA-Santa-Clara/Systems-Software-Engineer--Autonomous-Systems-Mapping---New-College-Graduate-2026_JR2020838", "Software", "2026 cohort in url", false},
	// #95
	{"Maritz", "Software Engineer 1", "Fenton, MO", "https://maritz.wd1.myworkdayjobs.com/Maritz/job/Fenton-MO/Software-Engineer-I_R15306", "Software", "", true},
	// #96
	{"Johnson Controls", "HVAC Equipment Systems Application Engineer 1", "Halethorpe, MD", "https://jci.wd5.myworkdayjobs.com/JCI/job/Linthicum-Maryland-United-States-of-America/HVAC-Equipment-Systems-Application-Engineer-I_WD30273247", "Software", "exclude:hvac", false},
	// #97
	{"Marriott International", "FLEX Associate Software Engineer - Customer Data Platform", "Bethesda, MD", "https://ejwl.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX/job/26085377", "Software", "", true},
	// #98
	{"Northrop Grumman", "Embedded Software Engineer Associate", "Hill AFB, UT", "https://ngc.wd1.myworkdayjobs.com/Northrop_Grumman_External_Site/job/United-States-Utah-Roy/SDS-Associate---Embedded-Software-Engineer---19013-_R10239748", "Hardware", "", true},
	// #99
	{"Ultra Intelligence and Communications", "Software Engineer Associate", "Austin, TX | Remote in USA | Huntsville, AL", "https://ultra.wd3.myworkdayjobs.com/uiccareers/job/Huntsville-USA/Associate-Software-Engineer_REQ-12218", "Software", "cross-post of #87", false},
	// #100
	{"Toast", "Software Engineer 1 - Fullstack", "Toronto, ON, Canada", "https://boards.greenhouse.io/embed/job_app?token=8046879", "Software", "location", false},
	// #101
	{"Leidos", "Junior Software Engineer - AI/ML Applications", "Huntsville, AL", "https://leidos.wd5.myworkdayjobs.com/External/job/Huntsville-AL/Junior-Software-Engineer---AI-ML-Applications_R-00187033", "Software", "", true},
	// #102
	{"Leidos", "Junior Software Engineer", "Huntsville, AL", "https://leidos.wd5.myworkdayjobs.com/External/job/Huntsville-AL/Junior-Software-Engineer_R-00187032", "Software", "", true},
	// #103
	{"Gen Digital", "AI & Machine Learning Engineer 1", "Mountain View, CA", "https://jobs.ashbyhq.com/gen-digital/b3ab78c4-af7c-4a95-9bbb-f6264e0c3adf/application", "AI/ML/Data", "", true},
	// #104
	{"GE Healthcare", "Field Engineer 1", "Madison, WI", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/Field-Engineer-1_R4042917-1", "Hardware", "exclude:field engineer", false},
	// #105
	{"GE Healthcare", "Field Engineer 1", "Normal, IL", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/FE-1-Bloomington--Illinois-Central-Illinois_R4042298-1", "Hardware", "exclude:field engineer", false},
	// #106
	{"University of Rochester", "Research Data Engineer 1", "Rochester, NY", "https://rochester.wd5.myworkdayjobs.com/UR_Staff/job/Rochester---NY/Research-Data-Engineer-I_R269057", "AI/ML/Data", "", true},
	// #107
	{"General Dynamics Information Technology", "UiPath Robotic Process Automation Developer Associate", "Louisiana", "https://gdit.wd5.myworkdayjobs.com/external_career_site/job/USA-LA-Home-Office-LAHOME/UiPath-Robotic-Process-Automation-Developer-Associate_RQ223293-1", "Software", "exclude:uipath", false},
	// #108
	{"GE Healthcare", "Field Service Engineer 1", "Seattle, WA", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/Remote/Field-Service-Engineer-1--Seattle-WA-area_R4041943-1", "Hardware", "exclude:field service", false},
	// #109
	{"Micron Technology", "New Grad Electrical Engineer - Computer Engineer - Engineering Automation", "Richardson, TX", "https://micron.wd1.myworkdayjobs.com/External/job/Richardson-TX/New-College-Grad---Computer-Engineer--Engineering-Automation_JR103691", "", "exclude:electrical", false},
	// #110
	{"Iridium Communications", "Site Engineer 1", "Fairbanks, AK", "https://careers-iridium.icims.com/jobs/5036/job?mobile=true&needsRedirect=false", "", "exclude:site engineer", false},
	// #111
	{"Micron Technology", "New College Grad - Product Yield Enhancement Engineer - High Bandwidth Memory", "Boise, ID", "https://micron.wd1.myworkdayjobs.com/External/job/Boise-ID---Main-Site/Product-Yield-Enhancement-Engineer--HBM_JR104804", "", "exclude:yield enhancement", false},
	// #112
	{"jumptrading", "Campus Full-Time Systems Engineer 2027 AMS", "Amsterdam", "https://www.jumptrading.com/hr/job?gh_jid=8008306", "", "location", false},
	// #113
	{"aurorainnovation", "Software Engineer I (Data Eng infra)", "Mountain View, California", "https://aurora.tech/jobs/8628066002?gh_jid=8628066002", "", "", true},
	// #114
	{"Denso", "Software Developer 1", "Battle Creek, MI", "https://hcwt.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX/job/24516", "", "", true},
	// #115
	{"Canonical", "Graduate Software Engineer - Open Source and Linux - Canonical Ubuntu", "Remote in UK", "https://job-boards.greenhouse.io/canonical/jobs/8055009", "", "location", false},
	// #116
	{"Peraton", "Entry-Level Full Stack Developer", "United States", "https://careers-peraton.icims.com/jobs/168438/job?mobile=true&needsRedirect=false", "", "", true},
	// #117
	{"wehrtyou", "Software Engineer (C++ or Python) – 2027 Grads", "Austin, TX, United States; Chicago, Illinois, United States; New York, NY, United States; Singapore", "https://www.hudsonrivertrading.com/careers/job/?gh_jid=8052122", "", "", true},
	// #118
	{"wehrtyou", "Algorithm Developer (Quant Researcher) – 2027 Grads", "New York, NY, United States; Singapore", "https://www.hudsonrivertrading.com/careers/job/?gh_jid=8052050", "", "", true},
	// #119
	{"wehrtyou", "Algorithm Developer (Quant Researcher) – 2027 PhDs", "New York, NY, United States; Singapore", "https://www.hudsonrivertrading.com/careers/job/?gh_jid=8059845", "", "exclude:phds", false},
	// #120
	{"andurilindustries", "Early Career Flight Test Engineer, Altius", "Costa Mesa, California, United States", "https://boards.greenhouse.io/andurilindustries/jobs/5185089007?gh_jid=5185089007", "", "exclude:flight test", false},
	// #121
	{"pinterest", "Software Engineer I, Fullstack", "Toronto, ON, CA", "https://www.pinterestcareers.com/jobs/?gh_jid=8055911", "", "location", false},
	// #122
	{"abridge", "Software Engineer- Early Careers", "SF Office", "https://jobs.ashbyhq.com/abridge/7d6ae2be-cd53-466c-8151-2dae2e87aace/application", "", "", true},
	// #123
	{"akunacapital", "Software Engineer (Entry-Level) - Python", "Chicago, IL", "https://www.akunacapital.com/careers/job/8013230/?gh_jid=8013230", "", "", true},
	// #124
	{"akunacapital", "Junior Quantitative Developer & Strategist", "Chicago, IL ", "https://www.akunacapital.com/careers/job/8016687/?gh_jid=8016687", "", "", true},
	// #125
	{"akunacapital", "Software Engineer (Entry-Level) - C++", "Chicago, IL", "https://www.akunacapital.com/careers/job/8013085/?gh_jid=8013085", "", "", true},
	// #126
	{"oldmissioncapital", "Software Engineer – 2027 Graduate Program (August Start)", "Chicago, IL, United States", "https://www.oldmissioncapital.com/careers/?gh_jid=7796048003", "", "", true},
	// #127
	{"andurilindustries", "2026 Early Career Test & Evaluation Engineer", "Costa Mesa, California, United States", "https://boards.greenhouse.io/andurilindustries/jobs/5185888007?gh_jid=5185888007", "", "2026 title, also test & evaluation", false},
	// #128
	{"rocketlab", "Stage Fluids Engineer I", "Long Beach, CA", "https://job-boards.greenhouse.io/rocketlab/jobs/7803167003", "", "exclude:fluids", false},
	// #129
	{"doordashusa", "Software Engineer I, Entry-Level (Graduation Date: Fall 2025-Summer 2026)", "New York, NY; San Francisco, CA; Los Angeles, CA; Seattle, WA; Sunnyvale, CA", "https://job-boards.greenhouse.io/doordashusa/jobs/7263610", "", "graduation window ends 2026", false},
	// #130
	{"sambanovasystems", "AI Systems Performance Engineer - New Graduate", "San Jose, California, United States", "https://sambanova.ai/sambanova-available-positions/?gh_jid=6115124004", "", "", true},
	// #131
	{"GHD", "Graduate Developer", "Pennsylvania | Houston, TX | Dallas, TX | Norridge, IL | Southfield, MI | Golden, CO", "https://ejov.fa.ca2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX/job/26934", "", "", true},
	// #132
	{"Northslope Technologies", "Forward Deployed Software Engineer New Grad", "NYC", "https://jobs.ashbyhq.com/northslope-technologies/80b82167-7101-4f78-9006-7755dd2ca01e/application", "", "", true},
	// #133
	{"RTX", "Software Engineer - Software Engineer I", "McKinney, TX", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-TX-MCKINNEY-513WD--2501-W-University-Dr--WING-D-BLDG/XMLNAME-2026-Raytheon-Full-Time---Software-Engineer-I---McKinney--TX--Onsite-_01853473", "", "2026 cohort in url", false},
	// #134
	// description wants summer 2026 grads, the filter can't see that. llm judge territory
	{"WhatNot", "Software Engineer New Grad", "NYC", "https://jobs.ashbyhq.com/whatnot/bc8f8c7f-2c4c-4f43-a238-953568c101b8/application", "", "", true},
	// #135
	{"Akuna Capital University", "Junior Quantitative Developer & Strategist", "Chicago, IL", "https://www.akunacapital.com/careers/job/8016687/?gh_jid=8016687", "", "cross-post of the greenhouse copy", false},
	// #136
	{"Akuna Capital University", "Entry Level Software Engineer - C++", "Chicago, IL", "https://www.akunacapital.com/careers/job/8013085/?gh_jid=8013085", "", "cross-post of the greenhouse copy", false},
	// #137
	{"HNTB", "Tolling Business Intelligence Developer 1", "Raleigh, NC", "https://hntb.wd5.myworkdayjobs.com/hntb_careers/job/Raleigh-NC/Tolling-Business-Intelligence-Developer-I_R-30750", "", "exclude:business intelligence", false},
	// #138
	{"Microchip Technology", "Engineer 1 - Software", "Santa Rosa, CA", "https://wd5.myworkdaysite.com/recruiting/microchiphr/External/job/CA---Santa-Rosa---Westwind/Engineer-I---Software_R2844-26", "", "", true},
	// #139
	{"Radius Limited", "Graduate C# Developer", "Northwich, UK", "https://jobs.smartrecruiters.com/RadiusLimited/744000137481419", "", "location", false},
	// #140
	{"DoorDash", "Software Engineer 1", "Seattle, WA | SF | LA | NYC | Sunnyvale, CA", "https://job-boards.greenhouse.io/doordashusa/jobs/7263610", "", "echo of a filtered-out posting, killed by req id", false},
	// #141
	{"Hudson River Trading", "Algorithm Developer New Grad - Quant Researcher", "NYC", "https://www.hudsonrivertrading.com/careers/job/?gh_jid=8052050", "", "cross-post, needs the display_names fold to HRT", false},
	// #142
	{"Micron Technology", "New College Grad - Design Engineer - Hbm", "Richardson, TX", "https://micron.wd1.myworkdayjobs.com/External/job/Richardson-TX/New-College-Grad---Design-Engineer--HBM_JR106212", "Hardware", "category Hardware", false},
	// #143
	{"Old Mission", "Software Engineer – Graduate Program - August Start", "Chicago, IL", "https://www.oldmissioncapital.com/careers/?gh_jid=7796048003", "", "cross-post of the greenhouse copy", false},
	// #144
	{"Dexcom", "Hardware Engineer 1", "San Diego, CA", "https://dexcom.wd1.myworkdayjobs.com/Dexcom/job/San-Diego-California/Hardware-Engineer-1_JR119691-1", "Hardware", "exclude:hardware engineer", false},
	// #145
	{"FOX", "Machine Learning Engineer 1", "NYC", "https://fox.wd1.myworkdayjobs.com/Domestic/job/New-York-New-York-USA/Machine-Learning-Engineer-I_R50033083", "", "", true},
	// #146
	{"Humana", "Junior Software Engineer Cloud Cost Optimization", "Boston, MA | Louisville, KY | Nashville, TN | Tampa, FL | Dallas, TX | Chicago, IL | Charlotte, NC | Fort Lauderdale, FL | NYC | Atlanta, GA", "https://humana.wd5.myworkdayjobs.com/humana_external_career_site/job/Louisville-KY/Junior-Software-Engineer--Cloud-Cost-Optimization_R-413873-1", "", "", true},
	// #147
	{"Milwaukee Tool", "Applied Machine Learning Engineer 1 - Advanced Engineering & Technology", "Brookfield, WI", "https://tti.wd1.myworkdayjobs.com/en-US/Milwaukee/job/Brookfield-WI/Applied-Machine-Learning-Engineer-I---Advanced-Engineering---Technology_R75643", "", "", true},
	// #148
	{"NVIDIA", "Compiler Engineer AI Inference New Grad", "Santa Clara, CA", "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite/job/US-CA-Santa-Clara/Compiler-Engineer--AI-Inference--New-College-Grad-2026_JR2021230", "", "2026 cohort in url", false},
	// #149
	{"Vishay Intertechnology", "Design Engineer 1", "Duluth, MN", "https://vishay.wd3.myworkdayjobs.com/VishayCareers/job/Duluth-MN/Design-Engineer-I_JR-18717", "Hardware", "category Hardware", false},
	// #150
	{"Yes Energy", "Data Engineer 1", "Boston, MA | Boulder, CO", "https://job-boards.greenhouse.io/yesenergy/jobs/5344484008", "", "", true},
	// #151
	{"NVIDIA", "Backend Compiler Engineer New Grad", "Canada | Santa Clara, CA", "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite/job/US-CA-Santa-Clara/Backend-Compiler-Engineer---New-College-Grad-2026_JR2021242", "", "2026 cohort in url", false},
	// #152
	{"Torc Robotics", "Hardware Engineer 1 - Sensors", "Ann Arbor, MI", "https://job-boards.greenhouse.io/torcrobotics/jobs/8622606002", "Hardware", "exclude:hardware engineer", false},
	// #153
	{"Varsity Brands", "Software Engineer 1", "Remote in USA", "https://careers.varsitybrands.com/global/en/job/JR114172", "", "", true},
	// #154
	{"SambaNova Systems", "AI Systems Performance Engineer New Grad", "San Jose, CA", "https://sambanova.ai/sambanova-available-positions/?gh_jid=6115124004", "", "cross-post of the greenhouse copy", false},
	// #155
	{"Google", "Software Engineer II, Early Career, Google Cloud AI Career Catalyst Program", "Sunnyvale, CA, USA", "https://www.google.com/about/careers/applications/jobs/results/138156162599002822", "", "", true},
	// #156
	{"Huntington Ingalls Industries", "Software Engineer 1", "Newport News, VA", "https://careers.huntingtoningalls.com/job/Newport-News-SOFTWARE-ENGINEER-1-Virg/1408046100/?ats=successfactors", "Software", "", true},
	// #157
	{"NVIDIA", "System Software Engineer New Grad - Dynamo-Triton Inference Server", "Remote in USA | Santa Clara, CA", "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite/job/US-CA-Santa-Clara/System-Software-Engineer--Dynamo-Triton-Inference-Server---New-College-Grad-2026_JR2020767", "Software", "2026 cohort in url", false},
	// #158
	{"Katalyst Space Technologies", "Software Engineer 1 - Model and Simulation", "Broomfield, CO", "https://job-boards.greenhouse.io/katalyst/jobs/6115352004", "Software", "", true},
	// #159
	{"mthree", "Junior Software Engineer", "Newark, DE", "https://job-boards.greenhouse.io/mthreerecruitingportal/jobs/4622899006", "Software", "", true},
	// #160
	{"shieldai", "Aerostructures Design Engineer I (R5155)", "United States", "https://jobs.lever.co/shieldai/b9e7d0a8-0f46-4d41-a09d-12d77470fc4a/apply", "", "exclude:aerostructures", false},
	// #161
	{"nuro", "Software Engineer, Performance - New Grad", "Mountain View, California (HQ)", "https://nuro.ai/careersitem?gh_jid=8064655", "", "", true},
	// #162
	{"fiveringsllc", "Campus Full Time 2027 - Software Developer", "New York", "https://job-boards.greenhouse.io/fiveringsllc/jobs/5349839008", "", "", true},
	// #163
	{"rocketlab", "RF Phased Array Engineer I/II", "Long Beach, CA", "https://job-boards.greenhouse.io/rocketlab/jobs/7803145003", "", "exclude:phased array", false},
	// #164
	{"Google", "Business Intelligence Developer 1 - Google Cloud", "Austin, TX | Chicago, IL | Boulder, CO | Atlanta, GA", "https://www.google.com/about/careers/applications/jobs/results/112117687143801542", "AI/ML/Data", "exclude:business intelligence", false},
	// #165
	{"Old Republic Title", "Associate Developer", "Orlando, FL", "https://oldrepublic.wd1.myworkdayjobs.com/oldrepublictitle/job/FL-Orlando-6545-Corporate-Ctr-Blvd/Associate-C--Developer_R-4196", "Software", "", true},
	// #166
	{"Cerebras", "Simulation Engineer New Grad", "Toronto, ON, Canada | Sunnyvale, CA", "https://jobs.ashbyhq.com/cerebras/bf6f81b2-f079-483a-9238-295a184b3f0f/application", "Software", "", true},
	// #167
	{"Intuit", "Software Engineer 1", "Charlotte, NC", "https://jobs.intuit.com/job/charlotte/software-engineer-i-credit-karma/27595/97793819216", "Software", "", true},
	// #168
	{"Nuro", "Software Engineer New Grad - Performance", "Mountain View, CA", "https://nuro.ai/careersitem?gh_jid=8064655", "Software", "cross-post of the greenhouse copy", false},
	// #169
	{"RTX", "Conversion Software Engineer 1", "Richardson, TX", "https://globalhr.wd5.myworkdayjobs.com/fr-CA/Private_Posting_No_TMP/job/US-TX-RICHARDSON-C17--1717-Cityline-Dr--CITYLINE-C17/XMLNAME-2027-Conversion-Software-Engineer-I--Onsite-_01858534", "Software", "", true},
	// #170
	{"RTX", "Software Engineer 1 - Tactical Communications", "Cedar Rapids, IA", "https://globalhr.wd5.myworkdayjobs.com/fr-CA/Private_Posting_No_TMP/job/US-IA-CEDAR-RAPIDS-137--855-35Th-St-NE--BLDG-137/Software-Engineer-1---Tactical-Communications--Onsite-_01851404-2", "Hardware", "", true},
	// #171
	{"Esri", "Software Development Engineer 1 - Agentic AI - Arcgis Enterprise", "West Redlands, Redlands, CA", "https://www.esri.com/careers/5186832007?gh_jid=5186832007", "AI/ML/Data", "", true},
	// #172
	{"InstaLILY", "Software Engineer 1 - General", "SF | NYC", "https://job-boards.greenhouse.io/instalilyai/jobs/4271757009", "Software", "", true},
	// #173
	{"Twitch", "Software Engineer 1 - Discovery", "SF", "https://job-boards.greenhouse.io/twitch/jobs/8623578002", "Software", "", true},
	// #174
	{"KBR", "Junior Systems Engineer - Digital Engineering Infrastructure", "Chantilly, VA", "https://kbr.wd5.myworkdayjobs.com/KBR_Careers/job/Chantilly-Virginia/Jr-Systems-Engineer--Digital-Engineering-Infrastructure-_R2125984", "AI/ML/Data", "", true},
	// #175
	{"Faros AI", "Software Engineer New Grad", "San Mateo, CA", "https://jobs.ashbyhq.com/faros-ai/622e1f1e-4a39-4e7c-8526-1189ca588066/application?embed=true", "Software", "", true},
	// #176
	{"CesiumAstro", "Embedded Software Engineer 1", "Austin, TX", "https://jobs.lever.co/CesiumAstro/1c793184-7d93-49e8-9e4c-ee2372c23096/apply", "Hardware", "", true},
	// #177
	{"KBR", "Junior Systems Engineer - Digital Engineering Methodology", "Chantilly, VA", "https://kbr.wd5.myworkdayjobs.com/KBR_Careers/job/Chantilly-Virginia/Jr-Systems-Engineer--Digital-Engineering-Methodology-_R2125985-1", "Software", "", true},
	// #178
	{"RTX", "Modeling Simulation & Analysis Engineer 1", "Tucson, AZ", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-AZ-TUCSON-9020--9020-S-Rita-Rd--BLDG-9020/XMLNAME-2026-Modeling-Simulation---Analysis-Engineer-I--Onsite_01825180", "AI/ML/Data", "2026 cohort in url", false},
	// #179
	{"GM financial", "Software Development Engineer 1 - React", "Irving, TX", "https://fa-exvu-saasfaprod1.fa.ocs.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_1/job/1901", "Software", "", true},
	// #180
	{"PathAI", "Software Engineer 1 - Fullstack", "Boston, MA", "https://www.pathai.com/careers/8466724002?gh_jid=8466724002", "Software", "", true},
	// #181
	{"Wyetech", "Software Engineer 1", "Annapolis Junction, MD", "https://jobs.lever.co/wyetechllc/b464498e-c95f-4f95-89ad-72d4ab61ab7e/apply", "Software", "", true},
	// #182
	{"Johnson & Johnson", "Technology Leadership Development Program - Tldp", "New Hope, PA | West Chester, PA | Bridgewater Township, NJ | Edison, NJ | Ambler, PA", "https://jj.wd5.myworkdayjobs.com/JJ/job/New-Brunswick-New-Jersey-United-States-of-America/Class-of-2027-Technology-Leadership-Development-Program--TLDP-_R-088676", "Software", "", true},
	// #183
	{"RTX", "Software Engineer 1", "McKinney, TX", "https://globalhr.wd5.myworkdayjobs.com/fr-CA/Private_Posting_No_TMP/job/US-TX-MCKINNEY-513WD--2501-W-University-Dr--WING-D-BLDG/XMLNAME-2026-Raytheon-Full-Time---Software-Engineer-I---McKinney--TX--Onsite-_01853473-1", "Software", "2026 cohort in url", false},
	// #184
	{"RTX", "Software Engineer 1 - Apnt", "Cedar Rapids, IA", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-IA-CEDAR-RAPIDS-112--400-Collins-Rd-NE--BLDG-112/Software-Engineer-I---APNT--Onsite-_01859795", "Hardware", "", true},
	// #185
	{"The Boeing Company", "Entry Level Support Engineering Data Specialist", "Tukwila, WA", "https://boeing.wd1.myworkdayjobs.com/external_subsidiary/job/USA---Tukwila-WA/Support-Engineering-Data-Specialist--Associate-_JR2026517149", "AI/ML/Data", "exclude:support engineering", false},
	// #186
	{"The Boeing Company", "Entry Level Support Engineering Data Specialist", "Tukwila, WA", "https://boeing.wd1.myworkdayjobs.com/EXTERNAL_CAREERS/job/USA---Tukwila-WA/Support-Engineering-Data-Specialist--Associate-_JR2026517149-1", "AI/ML/Data", "exclude:support engineering", false},
	// #187
	{"Torch Technologies", "Entry Level Engineer/Analyst", "Huntsville, AL", "https://starfish.wd501.myworkdayjobs.com/Careers/job/Huntsville-AL/Entry--Level-Engineer-Analyst_R1395", "AI/ML/Data", "exclude:engineer/analyst", false},
	// #188
	{"Vytalize Health", "Associate Data Engineer", "Remote in USA", "https://jobs.ashbyhq.com/Vytalize%20Health/3576b087-709e-4bf0-a016-6cc1c24b802c/application?embed=true", "AI/ML/Data", "", true},
	// #189
	{"Ensono", "Associate Machine Learning AI Engineer", "Remote in USA", "https://ensono.com/company/careers/jobs-board/?gh_jid=4711789005", "AI/ML/Data", "", true},
	// #190
	{"Cybernetic Labs", "Forward Deployed Engineer New Grad - Fde", "SF", "https://jobs.ashbyhq.com/netic/f2d170eb-c4c3-4715-9d2e-84dd4fe857c8/application?embed=true", "Software", "", true},
	// #191
	{"ICF International", "Associate ServiceNow Developer", "Reston, VA", "https://icf.wd5.myworkdayjobs.com/icfexternal_career_site/job/Reston-VA/Associate-ServiceNow-Developer--Secret-Clearance-Required-_R2602450", "Software", "exclude:servicenow", false},
	// #192
	{"Lightfield", "Software Engineer New Grad - Applied AI", "SF", "https://jobs.ashbyhq.com/Lightfield/fc93a467-773d-4805-b342-bf470950732d/application?embed=true", "Software", "", true},
	// #193
	{"Cybernetic Labs", "Full-Stack Software Engineer New Grad - Product", "SF", "https://jobs.ashbyhq.com/netic/bab5d1e5-e31b-42f0-9cef-334b1f17fed3/application?embed=true", "Software", "", true},
	// #194
	{"Cybernetic Labs", "Software Engineer New Grad - Agent Platform", "SF", "https://jobs.ashbyhq.com/netic/d9bcb6a2-0e54-4cb3-baec-43f2d74db18f/application?embed=true", "Software", "", true},
	// #195
	{"Micron Technology", "New College Grad - Product Yield Enhancement Engineer - High Bandwidth Memory", "Boise, ID", "https://micron.wd1.myworkdayjobs.com/External/job/Boise-ID---Main-Site/New-College-Grad---Product-Yield-Enhancement-Engineer--HBM_JR104805", "AI/ML/Data", "exclude:yield enhancement", false},
	// #196
	// description wants 1yr experience, another judge case
	{"Road Runner", "Junior Applications Engineer", "Downers Grove, IL", "https://wd1.myworkdaysite.com/en-US/recruiting/rrts/careers/job/Downers-Grove-IL/Junior-Applications-Engineer_R0002296-2", "Software", "", true},
	// #197
	{"Solar Turbines", "Entry Level Engineer Rotation - Solutions Platforms Engineering", "San Diego, CA", "https://cat.wd5.myworkdayjobs.com/en-US/solarturbines/job/San-Diego-California/XMLNAME-2027-Entry-Level-Rotation----Solutions-Platforms-Engineered_R0000381658", "Software", "", true},
	// #198
	{"GE Appliances", "Edison Engineering Development Program - Edison Engineering Development Program - Software", "Louisville, KY", "https://haier.wd3.myworkdayjobs.com/ge_appliances/job/USA-Louisville-KY/Edison-Engineering-Development-Program--EEDP----Software---July-2027_REQ-25807", "Hardware", "", true},
	// #199
	{"Solar Turbines", "Entry Level Gas Turbine Product Engineer - Gtpe", "San Diego, CA", "https://cat.wd5.myworkdayjobs.com/en-US/solarturbines/job/San-Diego-California/XMLNAME-2027-Entry-Level-Gas-Turbine-Product-Engineer--GTPE-_R0000382291", "Software", "exclude:gas turbine", false},
	// #200
	{"GE Appliances", "Engineering Development Program - Software", "Louisville, KY", "https://haier.wd3.myworkdayjobs.com/ge_appliances/job/USA-Louisville-KY/Edison-Engineering-Development-Program--EEDP----Software---January-2027_REQ-25806", "Hardware", "", true},
	// #201
	{"Commvault", "Entry-Level Software Engineer - AI/ML", "Shrewsbury, NJ", "https://job-boards.greenhouse.io/commvault/jobs/5233620008", "Software", "", true},
	// #202
	{"General Dynamics", "Engineer 1/2 - Communication Systems", "Groton, CT", "https://careers-gdeb.icims.com/jobs/19807/job?mobile=true&needsRedirect=false", "Software", "exclude:communication systems", false},
	// #203
	{"L3Harris Technologies", "Software Engineer Associate", "Salt Lake City, UT", "https://jobs.l3harris.com/job/Salt-Lake-City-Associate,-Software-Engineer-UT-84116/1408232900/?ats=successfactors", "Hardware", "", true},
	// #204
	{"L3Harris Technologies", "Software Engineering Associate", "Palm Bay, FL", "https://jobs.l3harris.com/job/Palm-Bay-Associate,-Software-Engineering-FL-32905/1409055400/?ats=successfactors", "Software", "", true},
	// #205
	{"L3Harris Technologies", "Associate Software Engineering", "Rochester, NY", "https://jobs.l3harris.com/job/Rochester-Associate,-Software-Engineering-NY-14610/1409372300/?ats=successfactors", "Hardware", "", true},
	// #206
	{"nuro", "Software Engineer, Performance - New Grad", "Mountain View, California (HQ)", "https://nuro.ai/careersitem?gh_jid=6972272", "", "", true},
	// #207
	{"rocketlab", "Ground Systems Engineer I, Controls", "Stennis Space Center, MS", "https://job-boards.greenhouse.io/rocketlab/jobs/7647932003", "", "exclude:ground systems", false},
	// #208
	{"rocketlab", "Test and Launch Operations Engineer I", "Wallops Island, VA", "https://job-boards.greenhouse.io/rocketlab/jobs/7807138003", "", "exclude:launch operations", false},
	// #209
	{"Adtran", "Software Engineer 1", "Norcross, GA", "https://adtran.wd3.myworkdayjobs.com/adtran/job/Atlanta-GA/Software-Engineer-I_R005697", "Hardware", "", true},
	// #210
	{"Esri", "Web Developer 1 - UI for Arcgis Enterprise", "West Redlands, Redlands, CA", "https://www.esri.com/careers/5188696007?gh_jid=5188696007", "Software", "", true},
	// #211
	{"GE Healthcare", "Software Engineer 1", "Orange, OH", "https://gehc.wd5.myworkdayjobs.com/GEHC_ExternalSite/job/OH05-01-Beachwood-Science-Park-Drive/Software-Engineer-I_R4040485-2", "Software", "", true},
	// #212
	{"Northrop Grumman", "Associate Software Engineer/Software Engineer", "Oklahoma City, OK", "https://ngc.wd1.myworkdayjobs.com/Northrop_Grumman_External_Site/job/United-States-Oklahoma-Oklahoma-City/XMLNAME-2027-Associate-Software-Engineer-Software-Engineer_R10240868", "Software", "", true},
	// #213
	{"Nuro", "Software Engineer New Grad - Performance", "Mountain View, CA", "https://nuro.ai/careersitem?gh_jid=6972272", "Software", "cross-post of the greenhouse copy", false},
	// #214
	{"Northrop Grumman", "Associate Software Engineer", "Huntsville, AL", "https://ngc.wd1.myworkdayjobs.com/Northrop_Grumman_External_Site/job/United-States-Alabama-Huntsville/XMLNAME-2027-Associate-Software-Engineer---Huntsville-AL_R10240786", "Software", "", true},
	// #215
	{"Northrop Grumman", "Associate Software Engineer", "Huntsville, AL", "https://ngc.wd1.myworkdayjobs.com/Northrop_Grumman_External_Site/job/United-States-Alabama-Huntsville/XMLNAME-2026-Associate-Software-Engineer---Huntsville-AL_R10240672-1", "Software", "2026 cohort in url", false},
	// #216
	{"Northrop Grumman", "Software Engineer Associate", "Melbourne, FL", "https://ngc.wd1.myworkdayjobs.com/Northrop_Grumman_External_Site/job/United-States-Florida-Melbourne/XMLNAME-2027-Associate-Software-Engineer---Software-Engineer_R10240764", "Software", "", true},
	// #217
	{"Antares Nuclear", "Reactor Software Engineer 1/2", "LA", "https://jobs.ashbyhq.com/Antares/78234003-fa70-41ab-b3c8-a2e703687f42/application?embed=true", "Hardware", "", true},
	// #218
	{"RTX", "Software Engineer 1", "El Segundo, CA", "https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-CA-EL-SEGUNDO-E01--2000-E-El-Segundo-Blvd--BLDG-E01/XMLNAME-2026-Raytheon-Full-Time--Software-Engineer-I---Onsite-_01852152", "Software", "2026 cohort in url", false},
	// #219
	{"Acrisure", "Software Engineer 1", "Uniondale, NY | Austin, TX | Grand Rapids, MI | Atlanta, GA", "https://acrisure.wd1.myworkdayjobs.com/Acrisure/job/333-Earle-Ovington-Blvd---Uniondale-NY/Software-Engineer-I_JR113871", "Software", "", true},
	// #220
	{"flyzipline", "Service Engineer I", "Kigali, Rwanda", "https://www.zipline.com/open-roles?gh_jid=7753358003", "", "location", false},
	// #221
	{"Western & Southern Financial Group", "Software Developer 1", "Cincinnati, OH", "https://careers-westernsouthern.icims.com/jobs/24908/job?mobile=true&needsRedirect=false", "Software", "", true},
	// #222
	{"Cencora", "Automation Developer 1", "Farmers Branch, TX", "https://myhrabc.wd5.myworkdayjobs.com/Global/job/Carrollton-TX/Automation-Developer-I_R2521185", "Software", "exclude:automation developer", false},
	// #223
	{"rocketlab", "IT Systems Engineer I", "Auckland, NZ", "https://job-boards.greenhouse.io/rocketlab/jobs/7790865003", "", "location, also it systems", false},
	// #224
	{"andurilindustries", "2026 Early Career Electrical Engineer, Battlespace Awareness Radar Team", "Fort Collins, Colorado, United States", "https://boards.greenhouse.io/andurilindustries/jobs/4747967007?gh_jid=4747967007", "", "2026 title, also electrical", false},
	// #225
	{"Nidec", "Software Engineer 1", "St. Louis, MO", "https://nidec.wd1.myworkdayjobs.com/nidec/job/North-AmericaUSAMissouriSt-Louis---WPE-MO/Software-Engineer-I_R0016664", "Hardware", "", true},
	// #226
	{"PNC Financial Services", "Software Developer Associate", "Dallas, TX | Pittsburgh, PA | Strongsville, OH", "https://pnc.wd5.myworkdayjobs.com/External/job/PA---Pittsburgh-15222/Software-Developer-Associate_R217594-1", "Software", "", true},
	// #227
	// guards the year rule: 2026 inside a req number must NOT trip it
	{"QTS", "AI Engineer 1", "Suwanee, GA", "https://qtsdatacenters.wd5.myworkdayjobs.com/qts/job/Suwanee-GA/AI-Engineer-I_R2026-1543", "Software", "", true},
	// #228
	{"S&C Electric Company", "Software Engineer 1", "Chicago, IL", "https://ejia.fa.us6.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_1001/job/106730", "Software", "", true},
	// #229
	// analytics rotation, meh, but no keyword kills it without collateral
	{"Sallie Mae", "Early Career Development Program Associate - Analytics", "Newark, DE", "https://sallie-mae.wd5.myworkdayjobs.com/Careers/job/Newark-DE/Associate--Analytics---Early-Career-Development-Program_R26_000512", "AI/ML/Data", "", true},
	// #230
	{"3RedPartners", "Graduate C++ Developer", "Chicago, IL", "https://job-boards.greenhouse.io/3redpartners/jobs/8631086002", "Software", "", true},
	// #231
	{"Esri", "Software Engineer 1 - Front-End Engineer for ArcGIS Enterprise", "West Redlands, Redlands, CA", "https://www.esri.com/careers/5190253007?gh_jid=5190253007", "Software", "", true},
	// #232
	{"NVIDIA", "Systems Software Engineer New Grad - Accelerated Kubernetes Performance and Scale", "Seattle, WA | Santa Clara, CA", "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite/job/US-CA-Santa-Clara/Systems-Software-Engineer--Accelerated-Kubernetes-Performance-and-Scale---New-College-Grad-2026_JR2020957", "Software", "2026 cohort in url", false},
	// #233
	{"Rocket Companies", "Android Mobile Software Developer 1", "Seattle, WA | SF | Detroit, MI", "https://quickenloans.wd5.myworkdayjobs.com/rocket_careers/job/Seattle-WA/Android-Mobile-Software-Developer-I_R-083752", "Software", "", true},
	// #234
	{"The Hartford ", "Associate Software Engineer - Tech Catalyst Program", "Columbus, OH", "https://thehartford.wd5.myworkdayjobs.com/Careers_External/job/Columbus-OH-Worth-Ave/Associate-Software-Engineer---Tech-Catalyst-Program--Columbus-_R2626154", "Software", "", true},
	// #235
	{"Magnatech", "Associate Developer", "Remote in USA", "https://magnatech.io/technology-careers/?gh_jid=4716011005", "Software", "", true},
	// #236 sirened 2026-07-20. right year, wrong job: a sales role from
	// the wide-net early-us google source riding the University Graduate
	// include. google doesn't send categories, keywords have to catch it
	{"Google", "Customer Growth Associate, Google Customer Solutions, University Graduate, 2027 Start (English)", "San Francisco, CA, USA | New York, NY, USA", "https://www.google.com/about/careers/applications/jobs/results/121628144896484038", "", "exclude:customer growth", false},
}

func TestRealAlertReplay(t *testing.T) {
	cfg, err := config.Load("../../config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f, err := filter.New(filter.Config{
		Include:                cfg.Filter.Include,
		ExcludeKeywords:        cfg.Filter.ExcludeKeywords,
		ExcludePatterns:        cfg.Filter.ExcludePatterns,
		ExcludeLocations:       cfg.Filter.ExcludeLocations,
		ExcludeCategories:      cfg.Filter.ExcludeCategories,
		CategoryRescueKeywords: cfg.Filter.CategoryRescueKeywords,
	})
	if err != nil {
		t.Fatal(err)
	}

	// replay in original arrival order so the cross-post dedup sees
	// the first copy before its echoes, same as production did
	seen := map[string]bool{}
	var kept, killed int
	for i, a := range realAlerts {
		job := sources.Job{
			Company:  a.company,
			Title:    a.title,
			Location: a.location,
			URL:      a.url,
			Category: a.category,
		}
		got := f.Match(job)
		k := crossPostKey(cfg.DisplayNames, job.Company, job.URL)
		if got && seen[k] {
			got = false
		}
		// prod dedups against every SEEN posting, not just alerted
		// ones, so filtered-out rows claim their key here too
		seen[k] = true
		if got != a.keep {
			if a.keep {
				t.Errorf("#%d GOOD alert lost: %s | %s | %s",
					i+1, a.company, a.title, a.location)
			} else {
				t.Errorf("#%d junk still alerting (expected kill: %s): %s | %s | %s",
					i+1, a.killedBy, a.company, a.title, a.location)
			}
		}
		if got {
			kept++
		} else {
			killed++
		}
	}
	t.Logf("replay: %d of %d real alerts survive, %d junk/dup killed", kept, len(realAlerts), killed)
}
