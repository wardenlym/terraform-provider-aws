package aws

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/terraform-providers/terraform-provider-aws/aws/internal/hashcode"
)

// How long to sleep if a limit-exceeded event happens
var routeTargetValidationError = errors.New("Error: more than 1 target specified. Only 1 of gateway_id, " +
	"egress_only_gateway_id, nat_gateway_id, instance_id, network_interface_id, local_gateway_id or " +
	"vpc_peering_connection_id is allowed.")

// AWS Route resource Schema declaration
func resourceAwsRoute() *schema.Resource {
	return &schema.Resource{
		Create: resourceAwsRouteCreate,
		Read:   resourceAwsRouteRead,
		Update: resourceAwsRouteUpdate,
		Delete: resourceAwsRouteDelete,
		Importer: &schema.ResourceImporter{
			State: func(d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
				idParts := strings.Split(d.Id(), "_")
				if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
					return nil, fmt.Errorf("unexpected format of ID (%q), expected ROUTETABLEID_DESTINATION", d.Id())
				}
				routeTableID := idParts[0]
				destination := idParts[1]
				d.Set("route_table_id", routeTableID)
				if strings.Contains(destination, ":") {
					d.Set("destination_ipv6_cidr_block", destination)
				} else {
					d.Set("destination_cidr_block", destination)
				}
				d.SetId(fmt.Sprintf("r-%s%d", routeTableID, hashcode.String(destination)))
				return []*schema.ResourceData{d}, nil
			},
		},

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(2 * time.Minute),
			Delete: schema.DefaultTimeout(5 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"destination_cidr_block": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				ValidateFunc: validation.Any(
					validation.StringIsEmpty,
					validateIpv4CIDRNetworkAddress,
				),
			},

			"destination_ipv6_cidr_block": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				ValidateFunc: validation.Any(
					validation.StringIsEmpty,
					validateIpv6CIDRNetworkAddress,
				),
				DiffSuppressFunc: suppressEqualCIDRBlockDiffs,
			},

			"destination_prefix_list_id": {
				Type:     schema.TypeString,
				Computed: true,
			},

			"gateway_id": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},

			"egress_only_gateway_id": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},

			"nat_gateway_id": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},

			"local_gateway_id": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},

			"instance_id": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},

			"instance_owner_id": {
				Type:     schema.TypeString,
				Computed: true,
			},

			"network_interface_id": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},

			"origin": {
				Type:     schema.TypeString,
				Computed: true,
			},

			"state": {
				Type:     schema.TypeString,
				Computed: true,
			},

			"route_table_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"transit_gateway_id": {
				Type:     schema.TypeString,
				Optional: true,
			},

			"vpc_peering_connection_id": {
				Type:     schema.TypeString,
				Optional: true,
			},
		},
	}
}

func resourceAwsRouteCreate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).ec2conn
	var numTargets int
	var setTarget string
	allowedTargets := []string{
		"egress_only_gateway_id",
		"gateway_id",
		"nat_gateway_id",
		"local_gateway_id",
		"instance_id",
		"network_interface_id",
		"transit_gateway_id",
		"vpc_peering_connection_id",
	}

	// Check if more than 1 target is specified
	for _, target := range allowedTargets {
		if len(d.Get(target).(string)) > 0 {
			numTargets++
			setTarget = target
		}
	}

	if numTargets > 1 {
		return routeTargetValidationError
	}

	createOpts := &ec2.CreateRouteInput{}
	// Formulate CreateRouteInput based on the target type
	switch setTarget {
	case "gateway_id":
		createOpts = &ec2.CreateRouteInput{
			RouteTableId: aws.String(d.Get("route_table_id").(string)),
			GatewayId:    aws.String(d.Get("gateway_id").(string)),
		}

		if v, ok := d.GetOk("destination_cidr_block"); ok {
			createOpts.DestinationCidrBlock = aws.String(v.(string))
		}

		if v, ok := d.GetOk("destination_ipv6_cidr_block"); ok {
			createOpts.DestinationIpv6CidrBlock = aws.String(v.(string))
		}

	case "egress_only_gateway_id":
		createOpts = &ec2.CreateRouteInput{
			RouteTableId:                aws.String(d.Get("route_table_id").(string)),
			DestinationIpv6CidrBlock:    aws.String(d.Get("destination_ipv6_cidr_block").(string)),
			EgressOnlyInternetGatewayId: aws.String(d.Get("egress_only_gateway_id").(string)),
		}
	case "nat_gateway_id":
		createOpts = &ec2.CreateRouteInput{
			RouteTableId:         aws.String(d.Get("route_table_id").(string)),
			DestinationCidrBlock: aws.String(d.Get("destination_cidr_block").(string)),
			NatGatewayId:         aws.String(d.Get("nat_gateway_id").(string)),
		}
	case "local_gateway_id":
		createOpts = &ec2.CreateRouteInput{
			RouteTableId:         aws.String(d.Get("route_table_id").(string)),
			DestinationCidrBlock: aws.String(d.Get("destination_cidr_block").(string)),
			LocalGatewayId:       aws.String(d.Get("local_gateway_id").(string)),
		}
	case "instance_id":
		createOpts = &ec2.CreateRouteInput{
			RouteTableId: aws.String(d.Get("route_table_id").(string)),
			InstanceId:   aws.String(d.Get("instance_id").(string)),
		}

		if v, ok := d.GetOk("destination_cidr_block"); ok {
			createOpts.DestinationCidrBlock = aws.String(v.(string))
		}

		if v, ok := d.GetOk("destination_ipv6_cidr_block"); ok {
			createOpts.DestinationIpv6CidrBlock = aws.String(v.(string))
		}

	case "network_interface_id":
		createOpts = &ec2.CreateRouteInput{
			RouteTableId:       aws.String(d.Get("route_table_id").(string)),
			NetworkInterfaceId: aws.String(d.Get("network_interface_id").(string)),
		}

		if v, ok := d.GetOk("destination_cidr_block"); ok {
			createOpts.DestinationCidrBlock = aws.String(v.(string))
		}

		if v, ok := d.GetOk("destination_ipv6_cidr_block"); ok {
			createOpts.DestinationIpv6CidrBlock = aws.String(v.(string))
		}

	case "transit_gateway_id":
		createOpts = &ec2.CreateRouteInput{
			RouteTableId:     aws.String(d.Get("route_table_id").(string)),
			TransitGatewayId: aws.String(d.Get("transit_gateway_id").(string)),
		}

		if v, ok := d.GetOk("destination_cidr_block"); ok {
			createOpts.DestinationCidrBlock = aws.String(v.(string))
		}

		if v, ok := d.GetOk("destination_ipv6_cidr_block"); ok {
			createOpts.DestinationIpv6CidrBlock = aws.String(v.(string))
		}

	case "vpc_peering_connection_id":
		createOpts = &ec2.CreateRouteInput{
			RouteTableId:           aws.String(d.Get("route_table_id").(string)),
			VpcPeeringConnectionId: aws.String(d.Get("vpc_peering_connection_id").(string)),
		}

		if v, ok := d.GetOk("destination_cidr_block"); ok {
			createOpts.DestinationCidrBlock = aws.String(v.(string))
		}

		if v, ok := d.GetOk("destination_ipv6_cidr_block"); ok {
			createOpts.DestinationIpv6CidrBlock = aws.String(v.(string))
		}

	default:
		return fmt.Errorf("A valid target type is missing. Specify one of the following attributes: %s", strings.Join(allowedTargets, ", "))
	}
	log.Printf("[DEBUG] Route create config: %s", createOpts)

	// Create the route
	var err error

	err = resource.Retry(d.Timeout(schema.TimeoutCreate), func() *resource.RetryError {
		_, err = conn.CreateRoute(createOpts)

		if isAWSErr(err, "InvalidParameterException", "") {
			return resource.RetryableError(err)
		}

		if isAWSErr(err, "InvalidTransitGatewayID.NotFound", "") {
			return resource.RetryableError(err)
		}

		if err != nil {
			return resource.NonRetryableError(err)
		}

		return nil
	})
	if isResourceTimeoutError(err) {
		_, err = conn.CreateRoute(createOpts)
	}
	if err != nil {
		return fmt.Errorf("Error creating route: %s", err)
	}

	var route *ec2.Route

	if v, ok := d.GetOk("destination_cidr_block"); ok {
		err = resource.Retry(d.Timeout(schema.TimeoutCreate), func() *resource.RetryError {
			route, err = resourceAwsRouteFindRoute(conn, d.Get("route_table_id").(string), v.(string), "")
			if err == nil {
				if route != nil {
					return nil
				} else {
					err = errors.New("Route not found")
				}
			}

			return resource.RetryableError(err)
		})
		if isResourceTimeoutError(err) {
			route, err = resourceAwsRouteFindRoute(conn, d.Get("route_table_id").(string), v.(string), "")
		}
		if err != nil {
			return fmt.Errorf("Error finding route after creating it: %s", err)
		}
		if route == nil {
			return fmt.Errorf("Unable to find matching route for Route Table (%s) and destination CIDR block (%s).", d.Get("route_table_id").(string), v)
		}
	}

	if v, ok := d.GetOk("destination_ipv6_cidr_block"); ok {
		err = resource.Retry(d.Timeout(schema.TimeoutCreate), func() *resource.RetryError {
			route, err = resourceAwsRouteFindRoute(conn, d.Get("route_table_id").(string), "", v.(string))
			if err == nil {
				if route != nil {
					return nil
				} else {
					err = errors.New("Route not found")
				}
			}

			return resource.RetryableError(err)
		})
		if isResourceTimeoutError(err) {
			route, err = resourceAwsRouteFindRoute(conn, d.Get("route_table_id").(string), "", v.(string))
		}
		if err != nil {
			return fmt.Errorf("Error finding route after creating it: %s", err)
		}
		if route == nil {
			return fmt.Errorf("Unable to find matching route for Route Table (%s) and destination IPv6 CIDR block (%s).", d.Get("route_table_id").(string), v)
		}
	}

	d.SetId(resourceAwsRouteID(d, route))

	return resourceAwsRouteRead(d, meta)
}

func resourceAwsRouteRead(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).ec2conn

	routeTableId := d.Get("route_table_id").(string)
	destinationCidrBlock := d.Get("destination_cidr_block").(string)
	destinationIpv6CidrBlock := d.Get("destination_ipv6_cidr_block").(string)

	route, err := resourceAwsRouteFindRoute(conn, routeTableId, destinationCidrBlock, destinationIpv6CidrBlock)
	if isAWSErr(err, "InvalidRouteTableID.NotFound", "") {
		log.Printf("[WARN] Route Table (%s) not found, removing from state", routeTableId)
		d.SetId("")
		return nil
	}
	if err != nil {
		return err
	}

	if route == nil {
		log.Printf("[WARN] Matching route not found, removing from state")
		d.SetId("")
		return nil
	}

	d.Set("destination_cidr_block", route.DestinationCidrBlock)
	d.Set("destination_ipv6_cidr_block", route.DestinationIpv6CidrBlock)
	d.Set("destination_prefix_list_id", route.DestinationPrefixListId)
	d.Set("gateway_id", route.GatewayId)
	d.Set("egress_only_gateway_id", route.EgressOnlyInternetGatewayId)
	d.Set("nat_gateway_id", route.NatGatewayId)
	d.Set("local_gateway_id", route.LocalGatewayId)
	d.Set("instance_id", route.InstanceId)
	d.Set("instance_owner_id", route.InstanceOwnerId)
	d.Set("network_interface_id", route.NetworkInterfaceId)
	d.Set("origin", route.Origin)
	d.Set("state", route.State)
	d.Set("transit_gateway_id", route.TransitGatewayId)
	d.Set("vpc_peering_connection_id", route.VpcPeeringConnectionId)

	return nil
}

func resourceAwsRouteUpdate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).ec2conn
	var numTargets int
	var setTarget string

	allowedTargets := []string{
		"egress_only_gateway_id",
		"gateway_id",
		"nat_gateway_id",
		"local_gateway_id",
		"network_interface_id",
		"instance_id",
		"transit_gateway_id",
		"vpc_peering_connection_id",
	}
	// Check if more than 1 target is specified
	for _, target := range allowedTargets {
		if len(d.Get(target).(string)) > 0 {
			numTargets++
			setTarget = target
		}
	}

	switch setTarget {
	//instance_id is a special case due to the fact that AWS will "discover" the network_interface_id
	//when it creates the route and return that data.  In the case of an update, we should ignore the
	//existing network_interface_id
	case "instance_id":
		if numTargets > 2 || (numTargets == 2 && len(d.Get("network_interface_id").(string)) == 0) {
			return routeTargetValidationError
		}
	default:
		if numTargets > 1 {
			return routeTargetValidationError
		}
	}

	var replaceOpts *ec2.ReplaceRouteInput
	// Formulate ReplaceRouteInput based on the target type
	switch setTarget {
	case "gateway_id":
		replaceOpts = &ec2.ReplaceRouteInput{
			RouteTableId:         aws.String(d.Get("route_table_id").(string)),
			DestinationCidrBlock: aws.String(d.Get("destination_cidr_block").(string)),
			GatewayId:            aws.String(d.Get("gateway_id").(string)),
		}
	case "egress_only_gateway_id":
		replaceOpts = &ec2.ReplaceRouteInput{
			RouteTableId:                aws.String(d.Get("route_table_id").(string)),
			DestinationIpv6CidrBlock:    aws.String(d.Get("destination_ipv6_cidr_block").(string)),
			EgressOnlyInternetGatewayId: aws.String(d.Get("egress_only_gateway_id").(string)),
		}
	case "nat_gateway_id":
		replaceOpts = &ec2.ReplaceRouteInput{
			RouteTableId:         aws.String(d.Get("route_table_id").(string)),
			DestinationCidrBlock: aws.String(d.Get("destination_cidr_block").(string)),
			NatGatewayId:         aws.String(d.Get("nat_gateway_id").(string)),
		}
	case "local_gateway_id":
		replaceOpts = &ec2.ReplaceRouteInput{
			RouteTableId:         aws.String(d.Get("route_table_id").(string)),
			DestinationCidrBlock: aws.String(d.Get("destination_cidr_block").(string)),
			LocalGatewayId:       aws.String(d.Get("local_gateway_id").(string)),
		}
	case "instance_id":
		replaceOpts = &ec2.ReplaceRouteInput{
			RouteTableId:         aws.String(d.Get("route_table_id").(string)),
			DestinationCidrBlock: aws.String(d.Get("destination_cidr_block").(string)),
			InstanceId:           aws.String(d.Get("instance_id").(string)),
		}
	case "network_interface_id":
		replaceOpts = &ec2.ReplaceRouteInput{
			RouteTableId:         aws.String(d.Get("route_table_id").(string)),
			DestinationCidrBlock: aws.String(d.Get("destination_cidr_block").(string)),
			NetworkInterfaceId:   aws.String(d.Get("network_interface_id").(string)),
		}
	case "transit_gateway_id":
		replaceOpts = &ec2.ReplaceRouteInput{
			RouteTableId:         aws.String(d.Get("route_table_id").(string)),
			DestinationCidrBlock: aws.String(d.Get("destination_cidr_block").(string)),
			TransitGatewayId:     aws.String(d.Get("transit_gateway_id").(string)),
		}
	case "vpc_peering_connection_id":
		replaceOpts = &ec2.ReplaceRouteInput{
			RouteTableId:           aws.String(d.Get("route_table_id").(string)),
			DestinationCidrBlock:   aws.String(d.Get("destination_cidr_block").(string)),
			VpcPeeringConnectionId: aws.String(d.Get("vpc_peering_connection_id").(string)),
		}
	default:
		return fmt.Errorf("An invalid target type specified: %s", setTarget)
	}
	log.Printf("[DEBUG] Route replace config: %s", replaceOpts)

	// Replace the route
	_, err := conn.ReplaceRoute(replaceOpts)
	return err
}

func resourceAwsRouteDelete(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).ec2conn

	deleteOpts := &ec2.DeleteRouteInput{
		RouteTableId: aws.String(d.Get("route_table_id").(string)),
	}
	if v, ok := d.GetOk("destination_cidr_block"); ok {
		deleteOpts.DestinationCidrBlock = aws.String(v.(string))
	}
	if v, ok := d.GetOk("destination_ipv6_cidr_block"); ok {
		deleteOpts.DestinationIpv6CidrBlock = aws.String(v.(string))
	}
	log.Printf("[DEBUG] Route delete opts: %s", deleteOpts)

	err := resource.Retry(d.Timeout(schema.TimeoutDelete), func() *resource.RetryError {
		log.Printf("[DEBUG] Trying to delete route with opts %s", deleteOpts)
		var err error
		_, err = conn.DeleteRoute(deleteOpts)
		if err == nil {
			return nil
		}

		if isAWSErr(err, "InvalidRoute.NotFound", "") {
			return nil
		}

		if isAWSErr(err, "InvalidParameterException", "") {
			return resource.RetryableError(err)
		}

		return resource.NonRetryableError(err)
	})
	if isResourceTimeoutError(err) {
		_, err = conn.DeleteRoute(deleteOpts)
	}
	if isAWSErr(err, "InvalidRoute.NotFound", "") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("Error deleting route: %s", err)
	}
	return nil
}

// Helper: Create an ID for a route
func resourceAwsRouteID(d *schema.ResourceData, r *ec2.Route) string {

	if r.DestinationIpv6CidrBlock != nil && *r.DestinationIpv6CidrBlock != "" {
		return fmt.Sprintf("r-%s%d", d.Get("route_table_id").(string), hashcode.String(*r.DestinationIpv6CidrBlock))
	}

	return fmt.Sprintf("r-%s%d", d.Get("route_table_id").(string), hashcode.String(*r.DestinationCidrBlock))
}

// resourceAwsRouteFindRoute returns any route whose destination is the specified IPv4 or IPv6 CIDR block.
// Returns nil if the route table exists but no matching destination is found.
func resourceAwsRouteFindRoute(conn *ec2.EC2, rtbid string, cidr string, ipv6cidr string) (*ec2.Route, error) {
	routeTableID := rtbid

	findOpts := &ec2.DescribeRouteTablesInput{
		RouteTableIds: []*string{&routeTableID},
	}

	resp, err := conn.DescribeRouteTables(findOpts)
	if err != nil {
		return nil, err
	}

	if len(resp.RouteTables) < 1 || resp.RouteTables[0] == nil {
		return nil, nil
	}

	if cidr != "" {
		for _, route := range (*resp.RouteTables[0]).Routes {
			if route.DestinationCidrBlock != nil && *route.DestinationCidrBlock == cidr {
				return route, nil
			}
		}

		return nil, nil
	}

	if ipv6cidr != "" {
		for _, route := range (*resp.RouteTables[0]).Routes {
			if cidrBlocksEqual(aws.StringValue(route.DestinationIpv6CidrBlock), ipv6cidr) {
				return route, nil
			}
		}

		return nil, nil
	}

	return nil, nil
}
